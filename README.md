# Cluster API Provider ELF Static IP

Cluster API Provider ELF Static IP 通过集成的方式为 [Kubernetes Cluster API Provider ELF](https://github.com/smartxworks/cluster-api-provider-elf) 提供 IPAM 管理。

## 使用

### 依赖的服务

* [M3IPAM](https://github.com/metal3-io/ip-address-manager) (v1.1.3) - Cluster, Machine
* [CAPE](https://github.com/smartxworks/cluster-api-provider-elf) (v0.3.6+) - ElfCluster, ElfMachine, ElfMachineTemplate
* [CAPI](https://github.com/kubernetes-sigs/cluster-api) (v1.2.0+) - IPPool, IPClaim, IPAddress

### 工作原理

当 ElfMachine 的网络设备的 networkType 为 *IPV4* 且未设置 ipAddrs 的时候，CAPE 会等待 ipAddrs 被设置才会继续创建虚拟机的流程。
Cluster API Provider ELF Static IP 的 controller 会 watch 所有的 ElfMachine，使用 ElfMachineTemplate 配置的 IPPool 给 ipAddrs 设置 IP。

### 通过 IPPool 名使用

```yaml
apiVersion: ipam.metal3.io/v1alpha1
kind: IPPool
metadata:
  name: ip-pool-1
  namespace: default
  labels:
spec:
  pools:
    - start: 10.255.160.10
      end: 10.255.160.20
      prefix: 16
      gateway: 10.255.0.1
  prefix: 16
  gateway: 10.255.0.1

apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: ElfMachineTemplate
metadata:
  name: elfk8s8-control-plane
  namespace: default
  labels:
    cluster.x-k8s.io/ip-pool-name: ip-pool-1
spec:
  template:
    spec:
      cloneMode: FastClone
      ha: true
      numCPUS: 4
      memoryMiB: 6144
      diskGiB: 20
      network:
        devices:
        - networkType: IPV4
          vlan: dd1f408f-7715-48c1-a817-13c3568f1d93_4cd00407-63ca-440b-80b7-ceacfccb8d08
      template: de6efbf8-fdae-4cad-9305-231c67d521a8

apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: ElfMachineTemplate
metadata:
  name: elfk8s8-worker
  namespace: default
  labels:
    cluster.x-k8s.io/ip-pool-name: ip-pool-1
spec:
  template:
    spec:
      cloneMode: FastClone
      ha: true
      numCPUS: 4
      memoryMiB: 6144
      network:
        devices:
        - networkType: IPV4
          vlan: dd1f408f-7715-48c1-a817-13c3568f1d93_4cd00407-63ca-440b-80b7-ceacfccb8d08
      template: de6efbf8-fdae-4cad-9305-231c67d521a8
```

### 通过 IPPool 标签使用

```yaml
apiVersion: ipam.metal3.io/v1alpha1
kind: IPPool
metadata:
  name: ip-pool-1
  namespace: default
  labels:
    cluster.x-k8s.io/ip-pool-group: ip-pool-group-1
    cluster.x-k8s.io/network-name: vm-network
spec:
  pools:
    - start: 10.255.160.10
      end: 10.255.160.20
      prefix: 16
      gateway: 10.255.0.1
  prefix: 16
  gateway: 10.255.0.1

---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: ElfMachineTemplate
metadata:
  name: elfk8s8-control-plane
  namespace: default
  labels:
    cluster.x-k8s.io/ip-pool-group: ip-pool-group-1
    cluster.x-k8s.io/network-name: vm-network
spec:
  template:
    spec:
      cloneMode: FastClone
      ha: true
      numCPUS: 4
      memoryMiB: 6144
      diskGiB: 20
      network:
        devices:
        - networkType: IPV4
          vlan: dd1f408f-7715-48c1-a817-13c3568f1d93_4cd00407-63ca-440b-80b7-ceacfccb8d08
      template: de6efbf8-fdae-4cad-9305-231c67d521a8

apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: ElfMachineTemplate
metadata:
  name: elfk8s8-worker
  namespace: default
  labels:
    cluster.x-k8s.io/ip-pool-group: ip-pool-group-1
    cluster.x-k8s.io/network-name: vm-network
spec:
  template:
    spec:
      cloneMode: FastClone
      ha: true
      numCPUS: 4
      memoryMiB: 6144
      network:
        devices:
        - networkType: IPV4
          vlan: dd1f408f-7715-48c1-a817-13c3568f1d93_4cd00407-63ca-440b-80b7-ceacfccb8d08
      template: de6efbf8-fdae-4cad-9305-231c67d521a8
```

### 默认 IPPool

如果设置了默认的 IPPool，当集群没有配置 IPPool 的时候，会选择默认的 IPPool。

```yaml
apiVersion: ipam.metal3.io/v1alpha1
kind: IPPool
metadata:
  name: ip-pool-1
  namespace: cape-system # 默认 IPPool 需要指定该命名空间
  labels:
    cluster.x-k8s.io/ip-pool-default: "" # 默认 IPPool 需要指定该标签
spec:
  pools:
    - start: 10.255.160.10
      end: 10.255.160.20
      prefix: 16
      gateway: 10.255.0.1
  prefix: 16
  gateway: 10.255.0.1
```

## 开发

### 部署 Cluster API Provider ELF Static IP

```sh
make deploy
```

### 本地运行

```sh
kubectl scale -n cape-system deployment.v1.apps/cape-ip-controller-manager --replicas 0

make run
```

### 测试

```sh
make test
```
