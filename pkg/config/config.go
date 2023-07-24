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
	"encoding/json"
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
var Meta3Memory map[string]int

func LoadData3Duration() {
	Meta3Duration = make(map[string]float64, 0)
	// Open the file
	currentDir, err := os.Getwd()
	fmt.Println("currentDir: ", currentDir)
	if err != nil {
		fmt.Println("无法获取当前工作目录:", err)
		panic("no current dir")
	}
	file, err := os.Open(currentDir + "/scaler/averages.csv")
	if err != nil {
		fmt.Println("Error opening file:", err)
		panic("no data3 duration")
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
	// for key, value := range Meta3Duration {
	// 	fmt.Printf("%s: %f\n", key, value)
	// }
}

type Data struct {
	Key            string `json:"key"`
	Runtime        string `json:"runtime"`
	MemoryInMb     int    `json:"memoryInMb"`
	TimeoutInSecs  int    `json:"timeoutInSecs"`
	InitDurationMs int    `json:"initDurationInMs"`
}

func LoadData3Memory() {
	Meta3Memory := make(map[string]int)
	// Open the file
	currentDir, err := os.Getwd()
	fmt.Println("currentDir: ", currentDir)
	if err != nil {
		fmt.Println("无法获取当前工作目录:", err)
		panic("no current dir")
	}
	file, err := os.Open(currentDir + "/scaler/metas")
	if err != nil {
		fmt.Println("Error opening file:", err)
		panic("no data3 memory")
	}
	defer file.Close()

	// Read the file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		data := &Data{}
		_ = json.Unmarshal([]byte(line), &data)
		Meta3Memory[data.Key] = data.MemoryInMb
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	// Print the map
	// for key, value := range Meta3Memory {
	// 	fmt.Printf("%s: %d\n", key, value)
	// }
}
