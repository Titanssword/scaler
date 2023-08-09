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

package model

import (
	"time"

	pb "github.com/AliyunContainerService/scaler/proto"
)

type Meta struct {
	pb.Meta
}

type Instance struct {
	Id                 string
	Slot               *Slot
	Meta               *Meta
	CreateTimeInMs     int64
	InitDurationInMs   int64
	Busy               bool
	LastIdleTime       time.Time
	CreateTime         int64
	ExecutionTimes     int64 // 该实例执行时间，如果被重复利用，则累计
	ExecutionStartTime int64
	ExecutionEndTime   int64
	SchedueTime        int64
	IdleTime           int64
}

type QpsEntity struct {
	CurrentTime int64
	QPS         int
}
