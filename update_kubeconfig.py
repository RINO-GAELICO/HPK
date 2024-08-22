import yaml
import os

# Define the path to the kubeconfig file
kubeconfig_path = '~/.k8sfs/kubernetes/admin.conf'
new_server_url = 'https://127.0.0.1:8443'

def update_kubeconfig(file_path, server_url):
    with open(file_path, 'r') as file:
        config = yaml.safe_load(file)

    # Update the server URL in the kubeconfig
    for cluster in config.get('clusters', []):
        cluster['cluster']['server'] = server_url

    with open(file_path, 'w') as file:
        yaml.dump(config, file, default_flow_style=False)

if __name__ == "__main__":
    if not os.path.exists(kubeconfig_path):
        print(f"File {kubeconfig_path} does not exist.")
    else:
        update_kubeconfig(kubeconfig_path, new_server_url)
        print(f"Kubeconfig updated with server URL: {new_server_url}")
