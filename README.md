<!-- omit in toc -->
# About

Toy VM Operator using kubebuilder for educational purposes presented at VMware
Code Connect 2020. Slides for the [talk (Youtube)](https://youtu.be/8Ex7ybi273g) can be found [here](./slides.pdf).

> **Note:** For a production-grade solution please see [Kubernetes Cluster API Provider vSphere](https://github.com/kubernetes-sigs/cluster-api-provider-vsphere#kubernetes-cluster-api-provider-vsphere). 

<!-- omit in toc -->
# Table of Content

- [What are We Trying to Achieve?](#what-are-we-trying-to-achieve)
- [Requirements for building the
  operator](#requirements-for-building-the-operator)
    - [Developer Software](#developer-software)
    - [Kubernetes Cluster](#kubernetes-cluster)
    - [vCenter Environment](#vcenter-environment)
- [Downloading the sample code](#downloading-the-sample-code)
- [Kubebuilder Scaffolding](#kubebuilder-scaffolding)
    - [Creating the directory](#creating-the-directory)
    - [Initializing the Directory](#initializing-the-directory)
    - [API Group Name, Version, Kind](#api-group-name-version-kind)
- [Custom Resource Definitions (CRD)](#custom-resource-definitions-crd)
    - [Concept](#concept)
    - [Our example: vmgroup_types.go](#our-example-vmgroup_typesgo)
    - [Version of CRDs](#version-of-crds)
    - [Creating the CRD](#creating-the-crd)
    - [Installing the CRD](#installing-the-crd)
- [Controller](#controller)
    - [Compiling](#compiling)
    - [Running the controller in development
      mode](#running-the-controller-in-development-mode)
    - [Deploying Controller on the
      Cluster](#deploying-controller-on-the-cluster)
- [Code Deep Dive](#code-deep-dive)
    - [The main.go](#the-maingo)
    - [Reconcile Method](#reconcile-method)
    - [The limit.go](#the-limitgo)
    - [Finalizer](#finalizer)
- [Advanced Topics (we could not cover)](#advanced-topics-we-could-not-cover)
- [Cleanup After Testing](#cleanup-after-testing)
- [Common Errors During this Exercise](#common-errors-during-this-exercise)
    - [go.mod incompability](#gomod-incompability)
    - [other go.mod errors](#other-gomod-errors)
    - [CRDs are not applied yet](#crds-are-not-applied-yet)
    - [Cluster not running / kubeconfig
      misconfigured](#cluster-not-running--kubeconfig-misconfigured)
    - [Lack of environment variables](#lack-of-environment-variables)
    - [Need the -insecure parameter](#need-the--insecure-parameter)
    - [Errors on make deployment](#errors-on-make-deployment)
    - [Controller not starting as a pod](#controller-not-starting-as-a-pod)
        - [Image Permissions](#image-permissions)
        - [CrashLoopBackOff](#crashloopbackoff)

# What are We Trying to Achieve?

Learn to build a sample Kubernetes Operator using kubebuilder. This operator
will create/delete Virtual Machines based on **declarative** desired state
configuration over **imperative** scripting/automation (i.e. PowerCLI or
`govc`). The declarative configuration is exemplified below by a standard
Kubernetes extensibility object (Customer Resource), in this case a `VmGroup`.
The `VmGroup` object represents a group of virtual machines managed by this
Kubernetes operator. Each group is reflected as a VM folder named
`<namespace>-<name>` with the number of Virtual Machines as specified on the
replica field. Each individual VM contains the number of CPU and amount of
memory as specified and they are created from the template name.

Example of Customer Resource:

```yaml
apiVersion: vm.codeconnect.vmworld.com/v1alpha1
kind: VmGroup
metadata:
  name: vmgroup-sample
  namespace: myoperator-vms
spec:
  cpu: 2
  memory: 1
  replicas: 3
  template: my-operator-template
```

At a high-level, this is the flow sequence of the operator's functionality:
1. User creates a custom resource (CR) via `kubectl` command under a Kubernetes
   namespace.
2. Operator is running on the cluster under the operator's namespace and it
   watches for these specific custom resources (CR) object.
3. Operator has vCenter credentials and it reconciles the state of the CR object
   with the vCenter using [govmomi library](https://github.com/vmware/govmomi).
4. Operator takes action: create, delete, scale up or scale down the VMs and its
   associated folder.

This CR will yield the following configuration on vCenter (VMfolder with VMs
under it):

![Image](/images/vcenter-final-result.png "vCenter with final result of a CRD
desired state.")


> **Note:** You do not need to be a Go developer to follow this step-by-step,
> but you will be exposed a bit in some Go constructs and how Go works.

# Requirements for building the operator

All examples on this demo are exemplified on a Mac OS X laptop but these
components should work on a standard Linux or Windows compatible `bash` shell.

## Developer Software

Install all the following:

- git client - Apple Xcode or any git command line 
- Go (v1.13+) - https://golang.org/dl/
- Docker Desktop - https://www.docker.com/products/docker-desktop
- Kind (Kubernetes in Docker) -  https://kind.sigs.K8s.io/docs/user/quick-start/
- Kubebuilder - https://go.kubebuilder.io/quick-start.html
- Kustomize - https://kubectl.docs.kubernetes.io/installation/
- [Optional but recommended] - Code editor such as
  [VScode](https://code.visualstudio.com/download) or
  [goland](https://www.jetbrains.com/go/download/#section=mac) (we will use
  VScode screenshots at this step-by-step)
- [Optional but recommended] - have access to a public registry such as quay.io
  or hub.docker.com. This is to push your controller container image to be
  deployable anywhere.

## Kubernetes Cluster

Any Kubernetes ("K8s") cluster 1.16 and above. For this exercise, we will be
using [Kind](https://kind.sigs.K8s.io/) which is a tool for running local
Kubernetes clusters using Docker container.

To start a kind cluster on your local machine, run the following command,
setting as an arbitrarily name for your cluster (this name will be used for
kubectl context):

```bash
kind create cluster --name operator-dev
```

By default, kind will start a cluster with the latest version of Kubernetes. The
following example starts a kind cluster with Kubernetes (K8s) 1.16 version:

```bash
kind create cluster --image=kindest/node:v1.16.4 --name operator-dev
```

By the way, you can have as many kind clusters you wish, as long as the cluster
name is unique. You can switch from one cluster to another using `kubectl config
use-context` . For this exercise, you will need only one cluster.

Before we start, let's make sure your kind cluster is responsive and create a
namespace where we will create our custom resource `VmGroup` objects:

```bash
$ kubectl create namespace myoperator-vms
namespace/myoperator-vms created
```

## vCenter Environment

You will need a vCenter, a user with privilege of creating and deleting Virtual
Machine Groups and VM folders. Other vCenter requirements:
- Your Kubernetes cluster must have direct access to the vCenter (no proxy, no
  jumpbox).
- You will need to create a "vm-operator" folder under the datacenter. The
  controller will create folders and VMs under this folder.
- Limitation of this academic code: 
  - vCenter cannot have more than one DataCenter.
  - You will need a default Resource Pool (DRS enabled).
- At least one VM template configured, preferrably under the "vm-operator"
  folder. The template name must be unique.

How to create a VM folder on any given vCenter:

![Image](/images/vcenter-new-vmfolder.png "vCenter new folder creation.")

Once the "vm-operator" folder is created, populate it with one or more
templates, in this case the template is called "vm-operator-template". It should
look like this:

![Image](/images/vcenter-vm-operator-folder-template.png "vCenter vm-operator
folder.")

# Downloading the Sample Code

To follow the next instructions, you will need to git clone this repo to your
machine. 

```bash
cd ~/go/src # or any folder you prefer for this excercise
git clone https://github.com/embano1/codeconnect-vm-operator.git
```

# Kubebuilder Scaffolding

"Scaffolding" is the first step of building an operator which kubebuilder will
initialize the operator from a brand-new directory.

## Creating the directory

For academic purposes, we will call the directory as "myoperator" but choose a
more meaningful name because this directory name will be used as part of the
default name of the K8s namespace and container of your operator (this is
default behavior and it can be changed).

```bash
mkdir myoperator
cd myoperator
```

At this time, we recommend you to open the code editor (using VScode as
screenshots from now on) with both directories: the new directory you just
created and the source code directory of this repository. It should look like
this:

![Image](/images/vscode-empty-directory.png "VScode Screenshot with two
directories.")


## Initializing the Directory

Now, define the go module name of your operator. This module name will be used
for package management inside this code base. We will call it myoperator as well
(same as the directory name).

```bash
$ go mod init myoperator
```

At this point, you will have a single file under myoperator folder: `go.mod`.

![Image](/images/vscode-go-module-name.png "VScode Screenshot with go.mod after
kubebuilder scaffolding.")

## API Group Name, Version, Kind

API Group, its versions and supported kinds are the part of the DNA of K8s. You
can read about these concepts
[here](https://kubernetes.io/docs/concepts/overview/kubernetes-api/).

For academic purposes, this example creates a kind `VmGroup` that belongs to the
API Group `vm.codeconnect.vmworld.com` with `v1alpha1` as the initial version.

These are all parameters for the kubebuilder scaffolding: please note the
parameters `--domain` and then `--group`, `--version` and `--kind`.

```bash
$ kubebuilder init --domain codeconnect.vmworld.com
# answer yes for both questions
$ kubebuilder create api --group vm --version v1alpha1 --kind VmGroup
Create Resource [y/n]
y
Create Controller [y/n]
y
```

Look again the now the content of myoperator folder: you will have a typical
directory with go code. The screenshot below shows the default `VmGroup` type
created under the `api` directory.

![Image](/images/vscode-kubebuilder-create-api.png "VScode Screenshot with
operator module name.")

Pay attention on the new version of the go.mod: kubebuilder populated all the
dependencies for the new controller you are building.


# Custom Resource Definitions (CRD)

## Concept

Custom Resource Definitions
[CRD](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
like the `VmGroup` introduced above are a way to extend Kubernetes. Any custom
controller/operator most likely requires at least one CRD for its functionality
(we will call it "root object"). When you design your operator, you will need to
spend time to define what kind of root object(s) you will need and the data that
each object will contain.

Typically, K8s objects have at least three sections: [metadata, spec and
status](https://kubernetes.io/docs/concepts/overview/working-with-objects/kubernetes-objects/)
besides the `kind` and `apiVersion` (as we learned in the last section, the kind
and apiVersion define the API Group, Version, Kind).

On `Spec` section is where you declare the desired state of the object. On
`Status` section is the current state of the object. Your operator's job is
reconciling these two sections in a loop.

## Our example: vmgroup_types.go

For our academic example, we want to define a root object type `VmGroup` that
will create a VM folder on vCenter with the number of VMs belonging to that
group specified as `replicas` field.

For such, we need to provide the following information to vCenter (besides the
name of the folder):
- `cpu`: how many CPUs that each VM will be created with. It is an integer.
- `memory`: how much memory (in GB) that each VM will be created with. Integer.
- `replicas`: How many VMs under the folder. Integer.
- `template`: the name of the VM template that VMs will be created. It is a
  string.

Scaffolding already created a `vmgroup_types.go` for us. However, it is empty
for these desired fields (on both spec and status sections). We will need to
populate these sections with our business logic.

```bash
$ cd ~/go/src
$ cp codeconnect-vm-operator/api/v1alpha1/vmgroup_types.go myoperator/api/v1alpha1/vmgroup_types.go
```

Look at the body of the `vmgroup_types.go`, the screenshot below has the new
`Spec` section.

![Image](/images/vscode-vmgroup-types-go.png "VScode Screenshot with
vmgroup_types.go")

## Version of CRDs

CRDs are evolving like any Kubernetes functionality. As of Kubernetes version
1.16, the default version of CRD is v1 which is the latest and stable version.
The latest version of CRD introduced many features, such as extensive validation
control of the CRDs. In this exercise, we want to onboard our controller with
the latest version of CRDs. For such, we need to set v1 version in default
kubebuilder Makefile (if you plan to run your operator on a prior version than
Kubernetes 1.16, please skip this part). 

```bash
$ cd ~/go/src/myoperator
# edit ~/go/src/myoperator/Makefile and change to the following line
$ vi Makefile
CRD_OPTIONS ?= "crd:preserveUnknownFields=false,crdVersions=v1,trivialVersions=true"
```

## Creating the CRD

The following will generate the CRD based on the spec/status from the
`vmgroup_types.go` file that we copied in last section:

```bash
make manifests && make generate
```

If you encounter issues in compiling, see section [common
errors](#common-errors-during-this-exercise) below.

Example of a clean run:

```bash
$ make manifests && make generate
go: creating new go.mod: module tmp
go: found sigs.K8s.io/controller-tools/cmd/controller-gen in sigs.K8s.io/controller-tools v0.2.5
/Users/rbrito/go//bin/controller-gen "crd:preserveUnknownFields=false,crdVersions=v1,trivialVersions=true" rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases
go: creating new go.mod: module tmp
go: found sigs.K8s.io/controller-tools/cmd/controller-gen in sigs.K8s.io/controller-tools v0.2.5
/Users/rbrito/go//bin/controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
```

The success criteria of the CRD generation is the yaml file
`config/crd/bases/vm.codeconnect.vmworld.com_vmgroups.yaml` being created.

## Installing the CRD

Before onboarding our newly created CRD, let's inspect the cluster to make sure
the `VmGroup` CRD does not exist yet. The following command lists all API Groups
of the cluster.

```bash
# show all existing API resources on this cluster
$ kubectl api-resources

# making sure they do not exist
$ kubectl api-resources --api-group=vm.codeconnect.vmworld.com

# empty output
NAME   SHORTNAMES   APIGROUP   NAMESPACED   KIND
```

Now install the CRD:

```bash
$ make install
kustomize build config/crd | kubectl apply -f -
customresourcedefinition.apiextensions.K8s.io/vmgroups.vm.codeconnect.vmworld.com created
```

Let's check again:

```bash
$ kubectl api-resources --api-group=vm.codeconnect.vmworld.com
NAME       SHORTNAMES   APIGROUP                     NAMESPACED   KIND
vmgroups   vg           vm.codeconnect.vmworld.com   true         VmGroup
```

Congrats, your cluster is now able to understand your CRD. Now you are able to
create `VmGroup` custom resources (CR).

Let's create one under our namespace myoperator-vms:

```bash
$ cat <<EOF | kubectl -n myoperator-vms create -f -
apiVersion: vm.codeconnect.vmworld.com/v1alpha1
kind: VmGroup
metadata:
  name: vmgroup-sample
spec:
  cpu: 2
  memory: 1
  replicas: 3
  template: vm-operator-template
EOF

# expected output
vmgroup.vm.codeconnect.vmworld.com/vmgroup-sample created
```

List the custom resource:

```bash
$ kubectl get vmgroups -n myoperator-vms
NAME             PHASE   CURRENT   DESIRED   CPU   MEMORY   TEMPLATE                LAST_MESSAGE
vmgroup-sample                     3         2     1        my-operator-template   
```
> **Note:** some fields are empty because we have not implemented our controller
> yet.

# Controller

Populate controller code base. Copy these files from this source repo to our
`myoperator` directory.

```bash
$ cd ~/go/src
$ cp codeconnect-vm-operator/controllers/* myoperator/controllers/
$ cp codeconnect-vm-operator/main.go myoperator/main.go
```

Since we copied from a different repo, make sure the Go import directives point
to our myoperator directory (see screenshot below the orginal `main.go`):

![Image](/images/vscode-old-main-go.png "VScode Screenshot with old main.go")

Change the import to the "myoperator" directory:

![Image](/images/vscode-new-main-go.png "VScode Screenshot with new main.go")

Repeat the same changes on `vmgroup_controller.go` and `vsphere.go` on your
`myoperator/controllers` directory.

**ATTENTION:** In `vsphere.go`, you will need to change the constant `vmPath` to
match your vCenter datacenter. The subfolder `vm` is standard under the
Datacenter object.

See example below:

![Image](/images/vscode-new-vsphere-go.png "VScode Screenshot with new
vsphere.go")

Since this code is for academic purposes, delete `suite_test.go` since we're not
writing any unit/integration tests.

```bash
cd ~/go/src/myoperator
rm controllers/suite_test.go
```

> **Note:** See [Advanced Topics](#advanced-topics-we-could-not-cover) below for
> areas which are out of scope for this excercise.


## Compiling

Compile the controller:

```bash
$ make manager
go fmt ./...
go vet ./...
go build -o bin/manager main.go
```

If you encounter errors, please check the section [Common
Errors](#common-errors-during-this-exercise) below.

## Running the Controller in "Development" Mode

For the first run, we will run our controller on the local machine. Create
environment variables for the vCenter connection.

```bash
export VC_HOST=10.186.34.28
export VC_USER=administrator@vsphere.local
export VC_PASS='Admin!23'
```

> **Note:** Make sure you change the following values for your vCenter.

Execute the controller locally. This is known as running the controller in
"development" mode (since it is not yet running inside a Kubernetes cluster). If
you encounter errors, please check the section [Common
Errors](#common-errors-during-this-exercise) below.

```bash
$ bin/manager -insecure

"level":"info","ts":1600289927.772952,"logger":"controller-runtime.metrics","msg":"metrics server is starting to listen","addr":":8080"}
{"level":"info","ts":1600289929.190078,"logger":"setup","msg":"starting manager"}
{"level":"info","ts":1600289929.190326,"logger":"controller-runtime.manager","msg":"starting metrics server","path":"/metrics"}
{"level":"info","ts":1600289929.190331,"logger":"controller","msg":"Starting EventSource","reconcilerGroup":"vm.codeconnect.vmworld.com","reconcilerKind":"VmGroup","controller":"vmgroup","source":"kind source: /, Kind="}
{"level":"info","ts":1600289929.293183,"logger":"controller","msg":"Starting Controller","reconcilerGroup":"vm.codeconnect.vmworld.com","reconcilerKind":"VmGroup","controller":"vmgroup"}
{"level":"info","ts":1600289929.293258,"logger":"controller","msg":"Starting workers","reconcilerGroup":"vm.codeconnect.vmworld.com","reconcilerKind":"VmGroup","controller":"vmgroup","worker count":1}
```

Pay attention on the following log lines, the controller identifies a reconcile
request for the `VmGroup` object that we have created earlier:

```bash
{"level":"info","ts":1600289929.293513,"logger":"controllers.VmGroup","msg":"received reconcile request for \"vmgroup-sample\" (namespace: \"myoperator-vms\")","vmgroup":"myoperator-vms/vmgroup-sample"}
{"level":"info","ts":1600289929.683297,"logger":"controllers.VmGroup","msg":"VmGroup folder does not exist, creating folder","vmgroup":"myoperator-vms/vmgroup-sample"}
{"level":"info","ts":1600289929.68333,"logger":"controllers.VmGroup","msg":"creating VmGroup in vCenter","vmgroup":"myoperator-vms/vmgroup-sample"}
{"level":"info","ts":1600289930.958067,"logger":"controllers.VmGroup","msg":"no VMs found for VmGroup, creating 3 replica(s)","vmgroup":"myoperator-vms/vmgroup-sample"}
{"level":"info","ts":1600289930.958116,"logger":"controllers.VmGroup","msg":"creating clone \"vmgroup-sample-replica-lbzgbaic\" from template \"vm-operator-template\"","vmgroup":"myoperator-vms/vmgroup-sample"}
{"level":"info","ts":1600289930.9581382,"logger":"controllers.VmGroup","msg":"creating clone \"vmgroup-sample-replica-mrajwwht\" from template \"vm-operator-template\"","vmgroup":"myoperator-vms/vmgroup-sample"}
{"level":"info","ts":1600289930.9581592,"logger":"controllers.VmGroup","msg":"creating clone \"vmgroup-sample-replica-hctcuaxh\" from template \"vm-operator-template\"","vmgroup":"myoperator-vms/vmgroup-sample"}
{"level":"info","ts":1600289938.199923,"logger":"controllers.VmGroup","msg":"received reconcile request for \"vmgroup-sample\" (namespace: \"myoperator-vms\")","vmgroup":"myoperator-vms/vmgroup-sample"}
{"level":"info","ts":1600289939.453033,"logger":"controllers.VmGroup","msg":"replica count in sync, checking power state","vmgroup":"myoperator-vms/vmgroup-sample"}
```

If you arrived at this point, it means that you have a functional controller
running on your desktop. Now it is time to generate a container image, a
deployment file and deploy on your kind cluster.

## Deploying Controller on the Cluster

Create a container image for the controller. Set the `IMG` parameter to point to
your container repository of preference. You can pick a custom name of your
image. 

```bash
$ make docker-build docker-push IMG=quay.io/embano1/codeconnect-vm-operator:latest
(...)
Successfully built 36e02974f03f
Successfully tagged quay.io/embano1/codeconnect-vm-operator:latest
docker push quay.io/embano1/codeconnect-vm-operator:latest
The push refers to repository [quay.io/embano1/codeconnect-vm-operator]
```

> **Note:** Do not forget to run docker login against your docker repository so
> you can push it.

The container is now built and pushed.

> **Note:** Make sure you set the image as public OR create a pull secret under
> the `myoperator-system` namespace.

Next, deploy the container on the cluster. For that, we will need a K8s
namespace, deployment manifest, RBAC roles, etc.

Luckly, `kubebuilder` and `kustomize` will automatically create these. However,
our custom controller expects an `-insecure` parameter and expects environmental
variables set: `VC_HOST, VC_USER and VC_PASS`. 

Thus, copy the `manager.yaml` and `manager_auth_proxy_patch.yaml` files from
this source directory to your myoperator directory:

```bash
cd ~/go/src
cp codeconnect-vm-operator/config/manager/manager.yaml myoperator/config/manager/manager.yaml
cp codeconnect-vm-operator/config/default/manager_auth_proxy_patch.yaml myoperator/config/default/manager_auth_proxy_patch.yaml 
```

Let's take a look on the `manager.yaml`  manifest.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: system
(...)
spec:
(...)
        env:
          - name: VC_USER
            valueFrom:
              secretKeyRef:
                name: vc-creds
                key: VC_USER
          - name: VC_PASS
            valueFrom:
              secretKeyRef:
                name: vc-creds
                key: VC_PASS
          - name: VC_HOST
            valueFrom:
              secretKeyRef:
                name: vc-creds
                key: VC_HOST
(...)
      volumes:
        - name: vc-creds
          secret:
            secretName: vc-creds
```

The configuration is telling the controller to set environment variables based
on keys from a Kubernetes secret "vc-creds".

Create the secret under the namespace "myoperator-system".

> **Note:** Use your own vCenter credentials. The secret must be created in the
> same namespace that the controller will run, in this case `myoperator-system`
> .

```bash
# create the secret in the target namespace
$ kubectl create ns myoperator-system
namespace/myoperator-system created
$ kubectl -n myoperator-system create secret generic vc-creds --from-literal='VC_USER=administrator@vsphere.local' --from-literal='VC_PASS=Admin!23' --from-literal='VC_HOST=10.186.34.28'
secret/vc-creds created
```

Deploy the controller:

```bash
$ cd ~/go/src/myoperator
$ make deploy IMG=quay.io/embano1/codeconnect-vm-operator:latest
(...)
deployment.apps/myoperator-controller-manager created
```

Check the logs of the controller:

```bash
$ kubectl get pods -n myoperator-system
NAME                                             READY   STATUS    RESTARTS   AGE
myoperator-controller-manager-7974b5bdbb-f6ptj   2/2     Running   0          84s

$ kubectl logs myoperator-controller-manager-7974b5bdbb-f6ptj -n myoperator-system manager
{"level":"info","ts":1600377978.6746724,"logger":"controller-runtime.metrics","msg":"metrics server is starting to listen","addr":"127.0.0.1:8080"}
{"level":"info","ts":1600377979.9868977,"logger":"setup","msg":"starting manager"}
```

> **Note:** If you are having issues on starting the controller, check the logs
> or see the section [Common Errors](#common-errors-during-this-exercise) below.

Finally, verify the existing `VmGroup` object and scale it to 5 replicas using
the `kubectl scale` command. 

```bash
$ kubectl get vmgroups -n myoperator-vms
NAMESPACE        NAME             PHASE     CURRENT   DESIRED   CPU   MEMORY   TEMPLATE               LAST_MESSAGE
myoperator-vms   vmgroup-sample   RUNNING   3         3         2     1        vm-operator-template   successfully reconciled VmGroup

# scaling the object to 5
$ kubectl scale --replicas=5 vmgroup vmgroup-sample -n myoperator-vms 
vmgroup.vm.codeconnect.vmworld.com/vmgroup-sample scaled

$ kubectl get vmgroups -n myoperator-vms
NAMESPACE        NAME             PHASE     CURRENT   DESIRED   CPU   MEMORY   TEMPLATE               LAST_MESSAGE
myoperator-vms   vmgroup-sample   RUNNING   5         5         2     1        vm-operator-template   successfully reconciled VmGroup
```

Confirm in vCenter that you see 5 VMs on the group's folder.

![Image](/images/vcenter-scaled-up.png "vCenter Screenshot with scaled VM
Group")


# Code Deep Dive

Now, let's take a look at the code itself.

This section will highlight some of the Go code that made the controller
functional.


## The main.go

Connection to the vCenter via govmomi and start the main loop:

```go
func main() {
(...)
    vc, err := newClient(ctx, vCenterURL, vcUser, vcPass, insecure)
    if err != nil {
        setupLog.Error(err, "could not connect to vCenter", "controller", "VmGroup")
        os.Exit(1)
    }
(...)
}

(...)

func newClient(ctx context.Context, vc, user, pass string, insecure bool) (*govmomi.Client, error) {
(...)
    u.User = url.UserPassword(user, pass)
    c, err := govmomi.NewClient(ctx, u, insecure)
    if err != nil {
        return nil, fmt.Errorf("could not get vCenter client: %v", err)
    }

    return c, nil
}
```

## Reconcile Method in `vmgroup_controller.go`

Reconcile method is responsible to receive the events (i.e new objects) and act
upon it. Snippet of the type object, request, checking the folder and creating
VM.


```go

// VmGroupReconciler reconciles a VmGroup object
type VmGroupReconciler struct {
    client.Client
    Finder       *find.Finder
    ResourcePool *object.ResourcePool
    VC           *govmomi.Client // owns vCenter connection
    Log          logr.Logger
    Scheme       *runtime.Scheme
}

(...)

func (r *VmGroupReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
    ctx := context.Background()
    log := r.Log.WithValues("vmgroup", req.NamespacedName)

  vg := &vmv1alpha1.VmGroup{}

(...)
    msg := fmt.Sprintf("received reconcile request for %q (namespace: %q)", vg.GetName(), vg.GetNamespace())
  log.Info(msg)
(...)
    // check if VmGroup folder exists
    _, err := getVMGroup(ctx, r.Finder, getGroupName(vg.Namespace, vg.Name))
(...)
    if !exists {
        log.Info("creating VmGroup in vCenter")
    _, err = createVMGroup(ctx, r.Finder, getGroupName(vg.Namespace, vg.Name))
(...)
        exists = true
    }
(...)
    // get replicas (VMs) for VmGroup
  vms, err := getReplicas(ctx, r.Finder, getGroupName(vg.Namespace, vg.Name))
  desired := vg.Spec.Replicas
  (...)
            eg.Go(func() error {
                defer lim.release()
                return cloneVM(egCtx, r.Finder, vg.Spec.Template, vmName, groupPath, r.ResourcePool, vg.Spec)
      })
(...)
```

## vCenter Concurrency Limitter

The `limit.go` file is responsible to make sure the controller does not
overwhelm the vCenter with multiple requests at once. It allows 3 requests, set
by the variable `defaultConcurrency` on `vsphere.go.`

```go
func newLimiter(concurrency int) *limiter {
    b := make(chan struct{}, concurrency)
    // fill bucket
    for token := 0; token < concurrency; token++ {
        b <- struct{}{}
    }
    return &limiter{
        bucket: b,
    }
}
```

## Finalizer

When the user deletes the CR object, the controller needs to delete the external
resources on the vCenter. A finalizer can be used to take care of issues during
deletion. A Kubernetes object cannot be deleted if it has one or more finalizers
registered.

```go
    // is object marked for deletion?
    if !vg.ObjectMeta.DeletionTimestamp.IsZero() {
        log.Info("VmGroup marked for deletion")
        // The object is being deleted
        if containsString(vg.ObjectMeta.Finalizers, finalizerID) {
            // our finalizer is present, so lets handle any external dependency
            if err := r.deleteExternalResources(ctx, r.Finder, vg); err != nil {
                // if fail to delete the external dependency here, return with error
                // so that it can be retried
                return ctrl.Result{RequeueAfter: defaultRequeue}, err
            }

            // remove our finalizer from the list and update it.
            vg.ObjectMeta.Finalizers = removeString(vg.ObjectMeta.Finalizers, finalizerID)
            if err := r.Update(context.Background(), vg); err != nil {
                return ctrl.Result{}, err
            }
        }
        // finalizer already removed
        return ctrl.Result{}, nil
    }

```

# Advanced Topics (we could not cover)

Feel free to enhance the code and submit PR(s) :)

- Code cleanup (DRY), unit/integration/E2E testing
- Sophisticated K8s/vCenter error handling and CR status representations
- Configurable target objects, e.g. datacenter, resource pool, cluster, etc.
- Supporting multi-cluster deployments and customizable namespace-to-vCenter
  mappings
- Multi-VC topologies (one-to-one for now)
- Generated object name verification and truncation within K8s/vCenter limits
- Advanced RBAC and security/role settings
- Controller local indexes for faster lookups
- vCenter object caching (and change notifications via property collector) to
  reduce network calls and round-trips
- Using hashing or any other form of compare function for efficient CR (event)
  change detection
- Using
  [expectations](https://github.com/elastic/cloud-on-K8s/blob/cf5de8b7fd09e55b74389128fbf917897b6bf17a/pkg/controller/common/expectations/expectations.go#L11)
  for more robust CR object handling (interleaving operations) detection  
- Smarter controller (re)queuing mechanisms
- vCenter task management (tracking) and async vCenter operations to not block
  too long during `Reconcile()`
- Robust vCenter session management (keep-alive, gracefully handle expired
  sessions, other forms of authentication against vCenter, etc.)
- Sophisticated leader election and availability (HA) concerns
- Fancy `kustomize`(ation)
- CRD API version upgrades
- Validation/defaulting with admission control/webhooks in the API server
- Advanced status reporting
  ([conditions](https://github.com/kubernetes-sigs/cluster-api/blob/master/docs/proposals/20200506-conditions.md),
  etc.)
- Production readiness (resources, certificate management, webhooks, quotas,
  [etc.](https://github.com/mercari/production-readiness-checklist))

# Cleanup After Testing

```bash
kubectl delete vg --all -A
...
kubectl delete crd vmgroups.vm.codeconnect.vmworld.com
customresourcedefinition.apiextensions.k8s.io "vmgroups.vm.codeconnect.vmworld.com" deleted
kubectl delete ns codeconnect-vm-operator-system

# or destroying the entire kind cluster
kind destroy cluster --name operator-dev
```

# Common Errors During this Exercise

Each section is a common error that you might encounter during this exercise.

## go.mod incompability

You might have some issues in compiling with mismatching modules. For example, I
had the following errors:

```
$ make manifests && make generate
go: finding module for package K8s.io/api/auditregistration/v1alpha1
go: finding module for package K8s.io/api/auditregistration/v1alpha1
../../../pkg/mod/K8s.io/kube-openapi@v0.0.0-20200831175022-64514a1d5d59/pkg/util/proto/document.go:24:2: case-insensitive import collision: "github.com/googleapis/gnostic/openapiv2" and "github.com/googleapis/gnostic/OpenAPIv2"
../../../pkg/mod/K8s.io/client-go@v11.0.0+incompatible/kubernetes/scheme/register.go:26:2: module K8s.io/api@latest found (v0.19.1), but does not contain package K8s.io/api/auditregistration/v1alpha1

# As documented on
# https://github.com/kubernetes/client-go/issues/741
```

You can fix it running:
```
go get -u K8s.io/client-go@v0.17.2 github.com/googleapis/gnostic@v0.3.1 ./...
# and editing go.mod for client-go and apimachinery to match versions:
# K8s.io/apimachinery v0.17.2
# K8s.io/client-go v0.17.2
# then running "go get " until there is a clean execution
```

## other `go.mod` errors

Another go compiling issue can be pkg/error undefined: 

```
$ make manager
go: creating new go.mod: module tmp
go: found sigs.K8s.io/controller-tools/cmd/controller-gen in sigs.K8s.io/controller-tools v0.2.5
/Users/rbrito/go//bin/controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
go fmt ./...
go vet ./...
# myoperator/controllers
controllers/vmgroup_controller.go:119:6: undefined: "github.com/pkg/errors".As
```

You can solve running:

```
 $ go get "github.com/pkg/errors"
go: github.com/pkg/errors upgrade => v0.9.1
```

## CRDs are not applied yet

```
$ kubectl create vmgroup
error: unable to recognize "STDIN": no matches for kind "VmGroup" in version "vm.codeconnect.vmworld.com/v1alpha1"
```

## Cluster not running / kubeconfig misconfigured

```
"level":"error","ts":1600288746.2496572,"logger":"controller-runtime.client.config","msg":"unable to get kubeconfig","error":"invalid configuration: no configuration has been provided, try setting KUBERNETES_MASTER environment variable","errorCauses":[{"error":"no configuration has been provided, try setting KUBERNETES_MASTER environment variable"}],"stacktrace":"github.com/go-logr/zapr.(*zapLogger).Error\n\t/Users/rbrito/go/pkg/mod/github.com/go-logr/zapr@v0.2.0/zapr.go:132\nsigs.K8s.io/controller-runtime/pkg/client/config.GetConfigOrDie\n\t/Users/rbrito/go/pkg/mod/sigs.K8s.io/controller-runtime@v0.6.3/pkg/client/config/config.go:159\nmain.main\n\t/Users/rbrito/go/src/myoperator/main.go:70\nruntime.main\n\t/usr/local/Cellar/go/1.14.2_1/libexec/src/runtime/proc.go:203"}
```

## Lack of environment variables

```
{"level":"error","ts":1600286316.549897,"logger":"setup","msg":"could not connect to vCenter","controller":"VmGroup","error":"could not parse URL (environment variables set?)","errorVerbose":"could not parse URL (environment variables set?)\nmain.newClient\n\t/Users/rbrito/go/src/myoperator/main.go:137\nmain.main\n\t/Users/rbrito/go/src/myoperator/main.go:90\nruntime.main\n\t/usr/local/Cellar/go/1.14.2_1/libexec/src/runtime/proc.go:203\nruntime.goexit\n\t/usr/local/Cellar/go/1.14.2_1/libexec/src/runtime/asm_amd64.s:1373","stacktrace":"github.com/go-logr/zapr.(*zapLogger).Error\n\t/Users/rbrito/go/pkg/mod/github.com/go-logr/zapr@v0.2.0/zapr.go:132\nmain.main\n\t/Users/rbrito/go/src/myoperator/main.go:92\nruntime.main\n\t/usr/local/Cellar/go/1.14.2_1/libexec/src/runtime/proc.go:203"}
```

## Need the -insecure parameter

If your vCenter does not have a signed cert, you will need to pass the -insecure
flag to the controller (manager binary).

```
"level":"error","ts":1600288944.8231418,"logger":"setup","msg":"could not connect to vCenter","controller":"VmGroup","error":"could not get vCenter client: Post \"https://10.186.34.28/sdk\": x509: cannot validate certificate for 10.186.34.28 because it doesn't contain any IP SANs","stacktrace":"github.com/go-logr/zapr.(*zapLogger).Error\n\t/Users/rbrito/go/pkg/mod/github.com/go-logr/zapr@v0.2.0/zapr.go:132\nmain.main\n\t/Users/rbrito/go/src/myoperator/main.go:92\nruntime.main\n\t/usr/local/Cellar/go/1.14.2_1/libexec/src/runtime/proc.go:203"}
```

## Errors on `toc`

Make deployment does not create the correct namespace using "myoperator-"
prefix. This is probably a bug on kustomize. Solution: create the
myoperator-system namespace manually before running `make deploy` (as specified
in the steps). 

```
$ make deploy IMG=quay.io/brito_rafa/codeconnect-vm-operator:latest
(...)
cd config/manager && kustomize edit set image controller=quay.io/brito_rafa/codeconnect-vm-operator:latest
kustomize build config/default | kubectl apply -f -
namespace/system created
Warning: kubectl apply should be used on resource created by either kubectl create --save-config or kubectl apply
customresourcedefinition.apiextensions.K8s.io/vmgroups.vm.codeconnect.vmworld.com configured
clusterrole.rbac.authorization.K8s.io/myoperator-manager-role created
clusterrole.rbac.authorization.K8s.io/myoperator-proxy-role created
clusterrole.rbac.authorization.K8s.io/myoperator-metrics-reader created
clusterrolebinding.rbac.authorization.K8s.io/myoperator-manager-rolebinding created
clusterrolebinding.rbac.authorization.K8s.io/myoperator-proxy-rolebinding created
Error from server (NotFound): error when creating "STDIN": namespaces "myoperator-system" not found
Error from server (NotFound): error when creating "STDIN": namespaces "myoperator-system" not found
Error from server (NotFound): error when creating "STDIN": namespaces "myoperator-system" not found
Error from server (NotFound): error when creating "STDIN": namespaces "myoperator-system" not found
make: *** [deploy] Error 1
```

## Controller not starting as a pod

### Image Permissions

Set your image as "public" access.

```bash
$ kubectl get pods -n myoperator-system
NAME                                             READY   STATUS             RESTARTS   AGE
myoperator-controller-manager-5bdbb5994c-hkwrh   1/2     ImagePullBackOff   0          89s
```

### CrashLoopBackOff

It could be a bunch of things, including the lack of "-insecure" parameter, the
lack of env variables, etc.

```bash
$ kubectl get pods -n myoperator-system
NAME                                             READY   STATUS             RESTARTS   AGE
myoperator-controller-manager-5bdbb5994c-csjqk   1/2     CrashLoopBackOff   1          13s

$ kubectl logs myoperator-controller-manager-5bdbb5994c-csjqk -n myoperator-system manager
```

For the -insecure parameter, edit the deployment:

```bash
kubectl edit deployment myoperator-controller-manager -n myoperator-system 

# add - -insecure
# under
# - --enable-leader-election
```
