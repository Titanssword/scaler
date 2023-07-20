package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	// Open the file
	file, err := os.Open("../../data/data_analyis/averages.csv")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	// Create a map to store the data
	data := make(map[string]float64)

	// Read the file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ",")
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if err == nil {
				data[key] = value
			}
		}
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	// Print the map
	for key, value := range data {
		fmt.Printf("%s: %f\n", key, value)
	}
}
