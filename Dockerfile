FROM chazapis/kubernetes-from-scratch:20230425

COPY generate-kubernetes-keys.sh /usr/local/bin/

COPY start-kubernetes.sh /usr/local/bin/

ENV K8SFS_HEADLESS_SERVICES=1
ENV K8SFS_RANDOM_SCHEDULER=1
ENV K8SFS_MOCK_KUBELET=1

ENTRYPOINT ["/usr/local/bin/start-kubernetes.sh"]

CMD ["8444"]

