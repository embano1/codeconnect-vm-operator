/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/pkg/errors"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/vim25/soap"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	vmv1alpha1 "codeconnect/operator/api/v1alpha1"
	"codeconnect/operator/controllers"
	// +kubebuilder:scaffold:imports
)

var (
	scheme        = runtime.NewScheme()
	setupLog      = ctrl.Log.WithName("setup")
	defaultResync = 5 * time.Minute // relist interval to sync CRs with external state (vC)
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = vmv1alpha1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var insecure bool

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&insecure, "insecure", false, "ignore any vCenter TLS cert validation error")
	flag.Parse()

	// ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	ctrl.SetLogger(zap.New(zap.UseDevMode(false)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "4837f5bf.codeconnect.vmworld.com",
		SyncPeriod:         &defaultResync,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	vCenterURL := os.Getenv("VC_HOST")
	vcUser := os.Getenv("VC_USER")
	vcPass := os.Getenv("VC_PASS")

	vc, err := newClient(ctx, vCenterURL, vcUser, vcPass, insecure)
	if err != nil {
		setupLog.Error(err, "could not connect to vCenter", "controller", "VmGroup")
		os.Exit(1)
	}

	finder := find.NewFinder(vc.Client)

	// TODO: make configurable, e.g. in spec
	dc, err := finder.DefaultDatacenter(ctx)
	if err != nil {
		setupLog.Error(err, "could not get default datacenter")
		os.Exit(1)
	}
	finder.SetDatacenter(dc)

	// TODO: make configurable, e.g. in spec
	rp, err := finder.DefaultResourcePool(ctx)
	if err != nil {
		setupLog.Error(err, "could not get default resource pool")
		os.Exit(1)
	}

	if err = (&controllers.VmGroupReconciler{
		Client:       mgr.GetClient(),
		VC:           vc,
		Finder:       finder,
		ResourcePool: rp,
		Log:          ctrl.Log.WithName("controllers").WithName("VmGroup"),
		Scheme:       mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VmGroup")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func newClient(ctx context.Context, vc, user, pass string, insecure bool) (*govmomi.Client, error) {
	u, err := soap.ParseURL(vc)
	if u == nil {
		return nil, errors.New("could not parse URL (environment variables set?)")
	}

	if err != nil {
		return nil, fmt.Errorf("could not parse vCenter client URL: %v", err)
	}

	u.User = url.UserPassword(user, pass)
	c, err := govmomi.NewClient(ctx, u, insecure)
	if err != nil {
		return nil, fmt.Errorf("could not get vCenter client: %v", err)
	}

	return c, nil
}
