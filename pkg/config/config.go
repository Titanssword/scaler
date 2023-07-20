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

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ClientAddr           string
	GcInterval           time.Duration
	IdleDurationBeforeGC *time.Duration
}

var DefaultConfig = Config{
	ClientAddr:           "127.0.0.1:50051",
	GcInterval:           10 * time.Second,
	IdleDurationBeforeGC: &BeforeGC,
}

var BeforeGC time.Duration = 5 * time.Minute

var GlobalMetaKey1 = []string{"nodes1", "roles1", "rolebindings1", "certificatesigningrequests1", "binding1", "csinodes1"}

var GlobalMetaKey2 = []string{"nodes2", "roles2", "rolebindings2", "certificatesigningrequests2", "binding2", "csinodes2"}

var Meta1Duration = map[string]float32{
	"certificatesigningrequests1": 29.958949,
	"csinodes1":                   30.000000,
	"nodes1":                      56.263234,
	"rolebindings1":               28.868354,
	"roles1":                      29.578194,
}

var Meta2Duration = map[string]float32{
	"certificatesigningrequests2": 29.958949,
	"csinodes2":                   30.000000,
	"nodes2":                      56.263234,
	"rolebindings2":               28.868354,
	"roles2":                      29.578194,
}

var Meta3Duration map[string]float64

func LoadData3() {
	// Open the file
	file, err := os.Open("../../data/data_analyis/averages.csv")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	// Read the file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ",")
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if err == nil {
				Meta3Duration[key] = value
			}
		}
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	// Print the map
	for key, value := range Meta3Duration {
		fmt.Printf("%s: %f\n", key, value)
	}
}
