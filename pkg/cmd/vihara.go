/*
Copyright 2020 The arhat.dev Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"arhat.dev/pkg/envhelper"
	"arhat.dev/pkg/kubehelper"
	"arhat.dev/pkg/log"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	"arhat.dev/vihara/pkg/apis"
	"arhat.dev/vihara/pkg/conf"
	"arhat.dev/vihara/pkg/constant"
	"arhat.dev/vihara/pkg/controller/maintenance"
)

func NewViharaCmd() *cobra.Command {
	var (
		appCtx       context.Context
		configFile   string
		config       = new(conf.ViharaConfig)
		cliLogConfig = new(log.Config)
	)

	viharaCmd := &cobra.Command{
		Use:           "vihara",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Use == "version" {
				return nil
			}

			var err error
			appCtx, err = conf.ReadConfig(cmd, &configFile, cliLogConfig, config)
			if err != nil {
				return err
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(appCtx, config)
		},
	}

	flags := viharaCmd.PersistentFlags()
	// config file
	flags.StringVarP(&configFile, "config", "c", constant.DefaultViharaConfigFile, "path to the vihara config file")
	// vihara
	flags.AddFlagSet(kubehelper.FlagsForControllerConfig("vihara", "", cliLogConfig, &config.Vihara.ControllerConfig))

	flags.DurationVar(&config.Maintenance.Schedule.PollInterval, "mt.schedule.pollInterval", 5*time.Second, "")
	flags.DurationVar(&config.Maintenance.Schedule.DefaultDelay, "mt.schedule.defaultDelay", time.Second, "")
	flags.DurationVar(&config.Maintenance.Schedule.DefaultDuration, "mt.schedule.defaultDuration", time.Hour, "")

	flags.StringVar(&config.Maintenance.Jobs.Namespace, "mt.jobs.namespace", "", "")
	flags.DurationVar(&config.Maintenance.Jobs.PollInterval, "mt.jobs.pollInterval", 5*time.Second, "")
	flags.DurationVar(&config.Maintenance.Jobs.DefaultStageTimeout, "mt.jobs.defaultStageTimeout", time.Hour, "")

	return viharaCmd
}

func run(appCtx context.Context, config *conf.ViharaConfig) error {
	// fallback to pod namespace if no job namespace defined
	if config.Maintenance.Jobs.Namespace == "" {
		config.Maintenance.Jobs.Namespace = envhelper.ThisPodNS()
	}

	// override with environment variable if set
	if constant.JobNS() != "" {
		config.Maintenance.Jobs.Namespace = constant.JobNS()
	}

	logger := log.Log.WithName("vihara")

	logger.I(fmt.Sprintf("using job namespace %q", config.Maintenance.Jobs.Namespace))
	logger.I("creating kube client for initialization")
	kubeClient, _, err := config.Vihara.KubeClient.NewKubeClient(nil, false)
	if err != nil {
		return fmt.Errorf("failed to create kube client from kubeconfig: %w", err)
	}

	var metricsHandler http.Handler
	if _, metricsHandler, err = config.Vihara.Metrics.CreateIfEnabled(true); err != nil {
		return fmt.Errorf("failed to create metrics provider: %w", err)
	}

	if metricsHandler != nil {
		mux := http.NewServeMux()
		mux.Handle(config.Vihara.Metrics.HTTPPath, metricsHandler)

		tlsConfig, err2 := config.Vihara.Metrics.TLS.GetTLSConfig(true)
		if err2 != nil {
			return fmt.Errorf("failed to get tls config for metrics listener: %w", err2)
		}

		srv := &http.Server{
			Handler:   mux,
			Addr:      config.Vihara.Metrics.Endpoint,
			TLSConfig: tlsConfig,
		}

		go func() {
			err2 = srv.ListenAndServe()
			if err2 != nil && !errors.Is(err2, http.ErrServerClosed) {
				panic(err2)
			}
		}()
	}

	if _, err = config.Vihara.Tracing.CreateIfEnabled(true, nil); err != nil {
		return fmt.Errorf("failed to create tracing provider: %w", err)
	}

	evb := record.NewBroadcaster()
	watchEventLogging := evb.StartLogging(func(format string, args ...interface{}) {
		logger.I(fmt.Sprintf(format, args...), log.String("source", "event"))
	})
	watchEventRecording := evb.StartRecordingToSink(
		&typedcorev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events(envhelper.ThisPodNS())})
	defer func() {
		watchEventLogging.Stop()
		watchEventRecording.Stop()
	}()

	logger.V("creating leader elector")
	elector, err := config.Vihara.LeaderElection.CreateElector("vihara", kubeClient,
		evb.NewRecorder(scheme.Scheme, corev1.EventSource{
			Component: "vihara",
		}),
		// on elected
		func(ctx context.Context) {
			// setup scheme for all custom resources
			logger.D("adding api scheme")
			if err = apis.AddToScheme(scheme.Scheme); err != nil {
				logger.E("failed to add api scheme", log.Error(err))
				os.Exit(1)
			}

			// create and start controller
			logger.D("creating controller")
			var controller *maintenance.Controller
			controller, err = maintenance.NewController(appCtx, config)
			if err != nil {
				logger.E("failed to create new controller", log.Error(err))
				os.Exit(1)
			}

			logger.I("starting controller")
			if err = controller.Start(); err != nil {
				logger.E("failed to start controller", log.Error(err))
				os.Exit(1)
			}
		},
		// on ejected
		func() {
			logger.E("lost leader election")
			os.Exit(1)
		},
		// on new leader
		func(identity string) {
			// TODO: react when new leader elected
		},
	)

	if err != nil {
		return fmt.Errorf("failed to create elector: %w", err)
	}

	logger.I("running leader election")
	elector.Run(appCtx)

	return fmt.Errorf("unreachable code")
}
