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

package config

import "time"

type Config struct {
	ClientAddr           string
	GcInterval           time.Duration
	IdleDurationBeforeGC *time.Duration
}

var DefaultConfig = Config{
	ClientAddr:           "127.0.0.1:50051",
	GcInterval:           8500 * time.Millisecond,
	IdleDurationBeforeGC: &BeforeGC,
}

var BeforeGC = 5 * time.Minute

var GlobalMetaKey1 = []string{"nodes1", "roles1", "rolebindings1", "certificatesigningrequests1", "binding1", "csinodes1"}

var GlobalMetaKey2 = []string{"nodes2", "roles2", "rolebindings2", "certificatesigningrequests2", "binding2", "csinodes2"}

func Contains(arr []string, target string) bool {
	for _, element := range arr {
		if element == target {
			return true
		}
	}
	return false
}
