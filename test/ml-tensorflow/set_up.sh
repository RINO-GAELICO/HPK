source /opt/cray/pe/lmod/lmod/init/bash && module load spin &&
export KUBE_PATH=~/.k8sfs/kubernetes/
export KUBECONFIG=${KUBE_PATH}/admin.conf
