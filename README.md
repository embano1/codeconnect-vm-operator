# codeconnect-vm-operator
Toy VM Operator for educational purposes and VMware CodeCon written in Go with kubebuilder

# what are we trying to achieve?

**declarative** desired state configuration over **imperative** scripting/automation (PowerCLI or `govc`)

```yaml
apiVersion: vm.codeconnect.vmworld.com/v1alpha1
kind: VmGroup
metadata:
  name: vmgroup-sample
spec:
  # Add fields here
  cpu: 10
  memory: 1
  replicas: 3
  template: vm-template
```

flow: kubectl -> operator (lib:govmomi) -> vCenter


# scaffolding

go mod init vmworld/codeconnect

kubebuilder init --domain codeconnect.vmworld.com
kubebuilder create api --group vm --version v1alpha1 --kind VmGroup

```bash
# show existing API resources before creating our CRD
kubectl api-resources
```

set crd v1 version in makefile: 

```Makefile
CRD_OPTIONS ?= "crd:preserveUnknownFields=false,crdVersions=v1,trivialVersions=true"
```

add spec/status in `vmgroup_types.go`

make manifests && make generate

# vmgroup_controller.go

add vc client to controller struct

```go
type VmGroupReconciler struct {
	client.Client
    VC *govmomi.Client
	Log     logr.Logger
	Scheme  *runtime.Scheme
}
```

# main.go

# first run of the manager

install CRD

```bash
# show API resources including our CRD
kubectl api-resources
```

```bash
kubectl create -f config/crd/bases/vm.codeconnect.vmworld.com_vmgroups.yaml
customresourcedefinition.apiextensions.k8s.io/vmgroups.vm.codeconnect.vmworld.com created

kubectl get vg
No resources found in default namespace.
```

export environment variables

```bash
export VC_USER=administrator@vsphere.local
export VC_PASS='Admin!23'
export VC_HOST=10.78.126.237
```

open watch in a separate window

```bash
watch -n1 "kubectl get vg,deploy,secret"
```

run the operator locally

```bash
make manager && bin/manager -insecure
```


# deploy to kubernetes

show the `manager.yaml`  manifest

```bash
# create the secret in the target namespace
kubectl create ns codeconnect-vm-operator-system
kubectl -n codeconnect-vm-operator-system create secret generic vc-creds --from-literal='VC_USER=administrator@vsphere.local' --from-literal='VC_PASS=Admin!23' --from-literal='VC_HOST=10.78.126.237'
```

delete `suite_test.go` since we're not writing any unit/integration tests and
deployment will otherwise fail

```bash
# build and deploy the manager to the cluster
make docker-build docker-push IMG=embano1/codeconnect-vm-operator:latest
make deploy IMG=embano1/codeconnect-vm-operator:latest
```

open watch in a separate window

```bash
watch -n1 "kubectl -n codeconnect-vm-operator-system get vg,deploy,secret"
```

create some VmGroups

```bash

```

fix the vg-2 template issue

```bash
kubectl patch vg vg-2 --type merge -p '{"spec":{"template":"vm-operator-template"}}'
```


# cleanup

```bash
kubectl delete vg --all -A
kubectl delete ns codeconnect-vm-operator-system
```

