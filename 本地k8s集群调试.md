# 在本地k8s环境，运行测试代码

背景：
ASK体验集群过期，自己申请使用费用感觉有些小贵，包括：弹性公网IP，NAT网关，负载均衡，这些如果选择是后付费的，即使不使用，也会计费。
单纯使用提交平台查看日志比较低。

> kubectl apply -f hack/serverless-simulaion.yaml 该命令的本质是在k8s集群，部署这俩服务，那是不是我自己提供一个k8s集群，然后让kubectl 来管理自己的集群，也可以完成相同的目标


## 选择 minikube 作为模拟k8s环境
参考文档:
[https://people.wikimedia.org/~jayme/k8s-docs/v1.16/zh/docs/setup/learning-environment/minikube/](https://people.wikimedia.org/~jayme/k8s-docs/v1.16/zh/docs/setup/learning-environment/minikube/)

选择对应的版本
[https://minikube.sigs.k8s.io/docs/start/](https://minikube.sigs.k8s.io/docs/start/)

forexample:

```
curl -LO https://storage.googleapis.com/minikube/releases/latest/minikube-darwin-amd64
sudo install minikube-darwin-amd64 /usr/local/bin/minikube
```

## 启动
`minikube start`

> 可能会有启动不来的情况，大概率是网络的问题

这时候执行

`minikube delete --all`

而后选择国内的源

`minikube start --image-mirror-country='cn'  --kubernetes-version=v1.23.1`

## check

`minikube status`

当你看到如下信息时，可以说明minikube已经启动起来了


```
minikube
type: Control Plane
host: Running
kubelet: Running
apiserver: Running
kubeconfig: Configured
```

确认下 kubectl 当前的配置

`kubectl config current-context`

看看是否是

`minikube`

可以参考这篇，配置多cluster
[https://kubernetes.io/docs/tasks/access-application-cluster/configure-access-multiple-clusters/](https://kubernetes.io/docs/tasks/access-application-cluster/configure-access-multiple-clusters/)

## 运行测试程序

- 启动

```
kubectl apply -f hack/serverless-simulaion.yaml
```
注意这里需要把scaler程序替换成自己的

- 删除
```
kubectl apply -f hack/serverless-simulaion.yaml
```

- 查看状态
```
kubectl get pods
```

```
kubectl describe pods
```

- 查看scaler日志

```
kubectl logs jobs/serverless-simulation scaler
```

- 查看simulator 容器日志:

```
kubectl logs jobs/serverless-simulation serverless-simulator
```

- 查看当前的数据统计

```
kubectl exec jobs/serverless-simulation -c scaler -- curl http://127.0.0.1:9000/
```
