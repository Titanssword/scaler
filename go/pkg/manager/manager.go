/*
Copyright 2023 The Alibaba Cloud Serverless Authors.
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

package manager

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/AliyunContainerService/scaler/go/pkg/config"
	"github.com/AliyunContainerService/scaler/go/pkg/model"
	scaler "github.com/AliyunContainerService/scaler/go/pkg/scaler"
)

type Manager struct {
	rw         sync.RWMutex
	schedulers map[string]scaler.Scaler
	config     *config.Config
}

func New(config *config.Config) *Manager {
	return &Manager{
		rw:         sync.RWMutex{},
		schedulers: make(map[string]scaler.Scaler),
		config:     config,
	}
}

func (m *Manager) GetOrCreate(metaData *model.Meta) scaler.Scaler {
	m.rw.RLock()
	if scheduler := m.schedulers[metaData.Key]; scheduler != nil {
		m.rw.RUnlock()
		return scheduler
	}
	m.rw.RUnlock()

	m.rw.Lock()
	if scheduler := m.schedulers[metaData.Key]; scheduler != nil {
		m.rw.Unlock()
		return scheduler
	}
	log.Printf("Create new scaler for app %s", metaData.Key)

	// 测试集1 5min
	_, okk1 := config.Meta1Duration[metaData.Key]
	if okk1 {
		newGCTime := time.Duration(5) * time.Minute
		m.config.IdleDurationBeforeGC = &newGCTime
	}
	// 测试集2 7min
	_, okk2 := config.Meta2Duration[metaData.Key]
	if okk2 {
		newGCTime := time.Duration(7) * time.Minute
		m.config.IdleDurationBeforeGC = &newGCTime
	}

	memory, ok := config.Meta3Memory[metaData.Key]
	initDuration, ok2 := config.Meta3InitDurationMs[metaData.Key]
	if ok && ok2 {
		//if data3InitDuration > 1000 {
		//var newGC = 15 * time.Minute
		//m.config.IdleDurationBeforeGC = &newGC
		//} else {
		//if data3Memory > 1024 {
		//var newGC = 3 * time.Minute
		//m.config.IdleDurationBeforeGC = &newGC
		//} else {
		//var newGC = 1 * time.Minute
		//m.config.IdleDurationBeforeGC = &newGC
		//}
		//}
		// a := data3InitDuration/data3Memory + 1
		newGCTime := time.Duration(30) * time.Second
		if initDuration < 1000 && memory > 1000 {
			newGCTime = time.Duration(10) * time.Second
		}
		m.config.IdleDurationBeforeGC = &newGCTime
	}

	scheduler := scaler.NewV2(metaData, m.config)
	m.schedulers[metaData.Key] = scheduler
	m.rw.Unlock()
	return scheduler
}

func (m *Manager) Get(metaKey string) (scaler.Scaler, error) {
	m.rw.RLock()
	defer m.rw.RUnlock()
	if scheduler := m.schedulers[metaKey]; scheduler != nil {
		return scheduler, nil
	}
	return nil, fmt.Errorf("scaler of app: %s not found", metaKey)
}
