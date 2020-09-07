package maintenance

import (
	"context"
	"fmt"

	"arhat.dev/pkg/log"
	"arhat.dev/pkg/queue"
	"arhat.dev/pkg/reconcile"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	clientbatchv1 "k8s.io/client-go/kubernetes/typed/batch/v1"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	kubecache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	viharaclient "arhat.dev/vihara/pkg/apis/vihara/generated/clientset/versioned"
	clientviharav1a1 "arhat.dev/vihara/pkg/apis/vihara/generated/clientset/versioned/typed/vihara/v1alpha1"
	viharainformers "arhat.dev/vihara/pkg/apis/vihara/generated/informers/externalversions"
	"arhat.dev/vihara/pkg/conf"
	"arhat.dev/vihara/pkg/util/manager"
)

func NewController(appCtx context.Context, config *conf.ViharaConfig) (*Controller, error) {
	kubeClient, kubeConfig, err := config.Vihara.KubeClient.NewKubeClient(nil, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube client for controller: %w", err)
	}

	viharaClient, err := viharaclient.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create vihara client for controller: %w", err)
	}

	jobNamespace := config.Maintenance.Jobs.Namespace

	mtInformerFactory := viharainformers.NewSharedInformerFactoryWithOptions(viharaClient, 0)
	mtJobInformerFactory := viharainformers.NewSharedInformerFactoryWithOptions(viharaClient, 0,
		viharainformers.WithNamespace(jobNamespace),
	)
	jobInformerFactory := informers.NewSharedInformerFactoryWithOptions(kubeClient, 0,
		informers.WithNamespace(jobNamespace),
	)

	ctrl := &Controller{
		config:      config,
		BaseManager: manager.NewBaseManager(appCtx, "vihara"),

		mtScheduleTQ:    queue.NewTimeoutQueue(),
		mtJobScheduleTQ: queue.NewTimeoutQueue(),

		jobNamespace: jobNamespace,

		kubeClient:  kubeClient,
		mtClient:    viharaClient.ViharaV1alpha1().Maintenance(),
		mtJobClient: viharaClient.ViharaV1alpha1().MaintenanceJobs(jobNamespace),
		jobClient:   kubeClient.BatchV1().Jobs(jobNamespace),
		nodeClient:  kubeClient.CoreV1().Nodes(),

		mtInformerFactory:    mtInformerFactory,
		mtJobInformerFactory: mtJobInformerFactory,
		jobInformerFactory:   jobInformerFactory,

		mtInformer:    mtInformerFactory.Vihara().V1alpha1().Maintenance().Informer(),
		mtJobInformer: mtJobInformerFactory.Vihara().V1alpha1().MaintenanceJobs().Informer(),
		jobInformer:   jobInformerFactory.Batch().V1().Jobs().Informer(),
	}

	// become the leader before proceeding
	mtEventBroadcaster := record.NewBroadcaster()
	_ = mtEventBroadcaster.StartLogging(func(format string, args ...interface{}) {
		ctrl.BaseManager.Log.I(fmt.Sprintf(format, args...), log.String("source", "event"))
	})
	_ = mtEventBroadcaster.StartRecordingToSink(
		&clientcorev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events(corev1.NamespaceAll)})
	ctrl.mtEventBroadcaster = mtEventBroadcaster
	ctrl.mtEventRecorder = mtEventBroadcaster.NewRecorder(
		scheme.Scheme, corev1.EventSource{Component: "maintenance"})
	ctrl.mtJobEventRecorder = mtEventBroadcaster.NewRecorder(
		scheme.Scheme, corev1.EventSource{Component: "maintenance-job"})

	ctrl.cacheSyncWaitFuncs = []kubecache.InformerSynced{
		ctrl.mtInformer.HasSynced,
		ctrl.mtJobInformer.HasSynced,
		ctrl.jobInformer.HasSynced,
	}

	ctrl.mtRec = reconcile.NewKubeInformerReconciler(appCtx, ctrl.mtInformer, reconcile.Options{
		Logger:          ctrl.Log.WithName("rec:mt"),
		BackoffStrategy: nil,
		Workers:         0,
		RequireCache:    true,
		Handlers: reconcile.HandleFuncs{
			OnAdded:    ctrl.onMaintenanceAdded,
			OnUpdated:  ctrl.onMaintenanceUpdated,
			OnDeleting: ctrl.onMaintenanceDeleting,
			OnDeleted:  ctrl.onMaintenanceDeleted,
		},
	})

	ctrl.mtJobRec = reconcile.NewKubeInformerReconciler(appCtx, ctrl.mtJobInformer, reconcile.Options{
		Logger:          ctrl.Log.WithName("rec:mt-job"),
		BackoffStrategy: nil,
		Workers:         0,
		RequireCache:    true,
		Handlers: reconcile.HandleFuncs{
			OnAdded:    ctrl.onMaintenanceJobAdded,
			OnUpdated:  ctrl.onMaintenanceJobUpdated,
			OnDeleting: ctrl.onMaintenanceJobDeleting,
			OnDeleted:  ctrl.onMaintenanceJobDeleted,
		},
	})

	ctrl.jobRec = reconcile.NewKubeInformerReconciler(appCtx, ctrl.jobInformer, reconcile.Options{
		Logger:          ctrl.Log.WithName("rec:job"),
		BackoffStrategy: nil,
		Workers:         0,
		RequireCache:    true,
		Handlers: reconcile.HandleFuncs{
			OnAdded:    ctrl.onJobAdded,
			OnUpdated:  ctrl.onJobUpdated,
			OnDeleting: ctrl.onJobDeleting,
			OnDeleted:  ctrl.onJobDeleted,
		},
	})

	return ctrl, nil
}

type Controller struct {
	config *conf.ViharaConfig
	*manager.BaseManager

	jobNamespace string

	mtEventBroadcaster record.EventBroadcaster
	mtEventRecorder    record.EventRecorder
	mtJobEventRecorder record.EventRecorder

	mtScheduleTQ    *queue.TimeoutQueue
	mtJobScheduleTQ *queue.TimeoutQueue

	kubeClient  kubeclient.Interface
	mtClient    clientviharav1a1.MaintenanceInterface
	mtJobClient clientviharav1a1.MaintenanceJobInterface
	jobClient   clientbatchv1.JobInterface
	nodeClient  clientcorev1.NodeInterface

	mtInformerFactory    viharainformers.SharedInformerFactory
	mtJobInformerFactory viharainformers.SharedInformerFactory
	jobInformerFactory   informers.SharedInformerFactory

	mtInformer    kubecache.SharedIndexInformer
	mtJobInformer kubecache.SharedIndexInformer
	jobInformer   kubecache.SharedIndexInformer

	cacheSyncWaitFuncs []kubecache.InformerSynced

	mtRec    *reconcile.KubeInformerReconciler
	mtJobRec *reconcile.KubeInformerReconciler
	jobRec   *reconcile.KubeInformerReconciler
}

func (c *Controller) Start() error {
	return c.OnStart(func() error {
		var (
			err    error
			stopCh = c.Context().Done()
		)

		c.mtInformerFactory.Start(stopCh)
		c.mtJobInformerFactory.Start(stopCh)
		c.jobInformerFactory.Start(stopCh)

		_ = c.mtRec.Start()
		_ = c.mtJobRec.Start()
		_ = c.jobRec.Start()

		_, err = c.mtInformerFactory.Vihara().V1alpha1().
			Maintenance().Lister().List(labels.Everything())
		if err != nil {
			return err
		}

		_, err = c.mtJobInformerFactory.Vihara().V1alpha1().
			MaintenanceJobs().Lister().MaintenanceJobs(c.jobNamespace).List(labels.Everything())
		if err != nil {
			return err
		}

		_, err = c.jobInformerFactory.Batch().V1().
			Jobs().Lister().Jobs(c.jobNamespace).List(labels.Everything())
		if err != nil {
			return err
		}

		ok := kubecache.WaitForCacheSync(stopCh, c.cacheSyncWaitFuncs...)
		if !ok {
			return fmt.Errorf("failed to sync resource cache")
		}

		// start timeout queue for job scheduling
		c.mtScheduleTQ.Start(stopCh)
		c.mtJobScheduleTQ.Start(stopCh)

		go c.mtRec.ReconcileUntil(stopCh)
		go c.mtJobRec.ReconcileUntil(stopCh)
		go c.jobRec.ReconcileUntil(stopCh)

		mtJobToBeScheduled := c.mtJobScheduleTQ.TakeCh()
		go func() {
			for td := range mtJobToBeScheduled {
				c.processMaintenanceJobSchedule(td.Key.(string))
			}
		}()

		mtToBeScheduled := c.mtScheduleTQ.TakeCh()
		for td := range mtToBeScheduled {
			c.processMaintenanceSchedule(td.Key.(string))
		}

		return nil
	})
}

func (c Controller) processMaintenanceSchedule(mtName string) {
	logger := c.Log.WithName("maintenance").WithFields(log.String("name", mtName), log.String("op", "scheduling"))

	logger.D("scheduling")
	err := c.scheduleMaintenance(mtName)
	if err != nil {
		logger.I("failed to schedule maintenance", log.Error(err))
	}

	if err = c.enqueueMaintenanceForScheduling(mtName, c.config.Maintenance.Schedule.PollInterval); err != nil {
		logger.E("failed to enqueue for scheduling", log.Error(err))
	}
}

func (c *Controller) processMaintenanceJobSchedule(mtJobName string) {
	logger := c.Log.WithName("maintenancejob").
		WithFields(log.String("name", mtJobName), log.String("op", "scheduling"))

	logger.D("scheduling")
	err := c.scheduleMaintenanceJob(c.jobNamespace, mtJobName)
	if err != nil {
		logger.I("failed to schedule maintenance job", log.Error(err))
	}

	if err = c.enqueueMaintenanceJobForScheduling(
		c.jobNamespace, mtJobName, c.config.Maintenance.Jobs.PollInterval); err != nil {
		logger.E("failed to enqueue for scheduling", log.Error(err))
	}
}
