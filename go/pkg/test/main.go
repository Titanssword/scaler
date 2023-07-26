package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type Data struct {
	Key            string `json:"key"`
	Runtime        string `json:"runtime"`
	MemoryInMb     int    `json:"memoryInMb"`
	TimeoutInSecs  int    `json:"timeoutInSecs"`
	InitDurationMs int    `json:"initDurationInMs"`
}

func main() {
	Meta3Memory := make(map[string]float64)
	// Open the file
	file, err := os.Open("../../data/data_training/dataSet_3/metas")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	// Read the file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		data := &Data{}
		_ = json.Unmarshal([]byte(line), &data)
		Meta3Memory[data.Key] = float64(data.MemoryInMb)
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	// Print the map
	for key, value := range Meta3Memory {
		fmt.Printf("%s: %f\n", key, value)
	}
}
