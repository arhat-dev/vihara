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

package conf

import (
	"time"

	"arhat.dev/pkg/confhelper"
	"arhat.dev/pkg/log"
)

type ViharaConfig struct {
	Vihara      ViharaAppConfig   `json:"vihara" yaml:"vihara"`
	Maintenance MaintenanceConfig `json:"maintenance" yaml:"maintenance"`
}

type ViharaAppConfig struct {
	confhelper.ControllerConfig `json:",inline" yaml:",inline"`
}

func (c *ViharaConfig) GetLogConfig() log.ConfigSet {
	return c.Vihara.Log
}

func (c *ViharaConfig) SetLogConfig(l log.ConfigSet) {
	c.Vihara.Log = l
}

type MaintenanceJobConfig struct {
	Namespace           string        `json:"namespace" yaml:"namespace"`
	DefaultStageTimeout time.Duration `json:"defaultTimeout" yaml:"defaultTimeout"`
	PollInterval        time.Duration `json:"pollInterval" yaml:"pollInterval"`
	//Timers         struct {
	//} `json:"timers" yaml:"timers"`
}

type MaintenanceScheduleConfig struct {
	DefaultDelay    time.Duration `json:"defaultDelay" yaml:"defaultDelay"`
	DefaultDuration time.Duration `json:"defaultDuration" yaml:"defaultDuration"`
	PollInterval    time.Duration `json:"pollInterval" yaml:"pollInterval"`
}

type MaintenanceConfig struct {
	Schedule MaintenanceScheduleConfig `json:"schedule" yaml:"schedule"`
	Jobs     MaintenanceJobConfig      `json:"jobs" yaml:"jobs"`
}
