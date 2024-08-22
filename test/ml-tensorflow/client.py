from __future__ import print_function

import base64
import io
import json
import sys
import numpy as np
from PIL import Image
import requests
from time import perf_counter
import os

# The server URL specifies the endpoint of your server running the ResNet
# model with the name "resnet" and using the predict interface.
# extract env variable RESNET_IP
RESNET_IP = os.environ['RESNET_IP']
SERVER_URL = 'http://{}:8501/v1/models/resnet:predict'.format(RESNET_IP)

# The image URL is the location of the image we should send to the server
IMAGE_PATH = 'dog.jpg'

# Path to the ImageNet classes file
IMAGENET_CLASSES_PATH = 'imagenet_classes.txt'

# Current Resnet model in TF Model Garden (as of 7/2021) does not accept JPEG
# as input
MODEL_ACCEPT_JPG = False

def load_imagenet_classes():
    """Load ImageNet class names from the file."""
    with open(IMAGENET_CLASSES_PATH, 'r') as f:
        return [line.strip() for line in f.readlines()]

def main():
    # Load the ImageNet class names
    class_names = load_imagenet_classes()

    # Download the image
    with open(IMAGE_PATH, 'rb') as image_file:
        image_data = image_file.read()

    # Compose a JSON Predict request (send JPEG image in base64).
    jpeg_bytes = base64.b64encode(image_data).decode('utf-8')
    predict_request = '{"instances" : [{"b64": "%s"}]}' % jpeg_bytes

    warm_up_num_requests = 3
    # Send few requests to warm-up the model.
    for _ in range(warm_up_num_requests):
        response = requests.post(SERVER_URL, data=predict_request)
        response.raise_for_status()

    # Send few actual requests and report average latency.
    NUM_ITERATIONS = 5
    all_throughputs_results = {}
    for iteration in range(NUM_ITERATIONS):
    
        NUMBER_OF_REQUESTS = int(sys.argv[1])
        predictions = []
        t_0 = perf_counter()
        for _ in range(NUMBER_OF_REQUESTS):
            response = requests.post(SERVER_URL, data=predict_request)
            response.raise_for_status()
            # print("Full Response JSON:", response.json())
            prediction = response.json()['predictions'][0]['probabilities']
            predictions.append(prediction)
        t_n = perf_counter()
        
        throughput = NUMBER_OF_REQUESTS / (t_n - t_0)
        print(f"Throughput: {throughput} requests per second")
        
        throughputs_results = {
                "throughput": throughput,
                "start_time": t_0,
                "end_time": t_n
            }
        
        all_throughputs_results[iteration] = throughputs_results
        

        max_index = prediction.index(max(prediction))

        print(f"The highest probability is : {max(prediction)}")
        avg_latency = ((t_n-t_0) * 1000) / NUMBER_OF_REQUESTS
        print(f'Average latency: {avg_latency:.2f} ms')
        
        # use the index to get the class name
        print(f"Class name: {class_names[max_index-1]}")
    
    output_file_name_throughput = "throughput_{}_requests.json".format(NUMBER_OF_REQUESTS)
    with open(output_file_name_throughput, "w") as f:
        json.dump(all_throughputs_results, f)


if __name__ == '__main__':
    main()
