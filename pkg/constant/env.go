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

package constant

import (
	"os"

	"arhat.dev/pkg/envhelper"
)

const (
	EnvKeyPodUID         = "POD_UID"
	EnvKeyWatchNamespace = "WATCH_NAMESPACE"
	EnvKeyJobNamespace   = "JOB_NAMESPACE"
)

var (
	podUID  string
	watchNS string
	jobNS   string
)

func init() {
	var ok bool
	podUID = os.Getenv(EnvKeyPodUID)

	watchNS, ok = os.LookupEnv(EnvKeyWatchNamespace)
	if !ok {
		watchNS = envhelper.ThisPodNS()
	}

	jobNS, _ = os.LookupEnv(EnvKeyJobNamespace)
}

func ThisPodUID() string {
	return podUID
}

func WatchNS() string {
	return watchNS
}

func JobNS() string {
	return jobNS
}
