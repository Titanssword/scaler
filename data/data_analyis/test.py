import json
import pandas as pd
import numpy as np
import matplotlib.pyplot as plt
from datetime import datetime

with open('../data_training/dataSet_3/metas') as f:
    data = f.readlines()

metas = []
for line in data:
    request = json.loads(line)
    metas.append(request)

with open('../data_training/dataSet_3/requests') as f:
    data2 = f.readlines()

requests = []
for line in data2:
    request = json.loads(line)
    requests.append(request)

filtered_data_set1 = [entry for entry in metas if entry["memoryInMb"] == 8192]

# Filter Data Set 2 to include only requests with memoryInMb=128
filtered_data_set2 = [entry for entry in requests if any(item["key"] == entry["metaKey"] for item in filtered_data_set1)]

start_times = [entry["startTime"] for entry in filtered_data_set2]
date_times = [datetime.fromtimestamp(ts / 1000) for ts in start_times]

# Count the number of requests for each unique hour
unique_hours, request_counts = np.unique([dt.replace(minute=0, second=0, microsecond=0) for dt in date_times], return_counts=True)

# Create the plot using Matplotlib
plt.bar(unique_hours, request_counts, align='center', width=0.4)
plt.xlabel("Time (Hours)")
plt.ylabel("Request Count")
plt.title("Request Count vs. Time (MemoryInMb=128)")
plt.xticks(rotation=45)
plt.tight_layout()
plt.show()

# # Extract initDurationInMs values
# init_duration_values = [entry["memoryInMb"] for entry in python_entries]
#
# # Define bin size
# bin_size = 128
#
# # Create bins using NumPy
# bins = np.arange(0, max(init_duration_values) + bin_size, bin_size)
#
# # Create the histogram using Matplotlib
# plt.hist(init_duration_values, bins=bins, edgecolor='black')
# plt.xlabel("memoryInMb")
# plt.ylabel("Frequency")
# plt.title("Python Runtimes - Frequency of memoryInMb")
# plt.grid(True)
# plt.show()

