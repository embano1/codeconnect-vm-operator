package controllers

import (
	"context"
	"strings"

	"github.com/pkg/errors"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"

	"codeconnect/operator/api/v1alpha1"
)

const (
	// TODO: make configurable
	vmPath = "/vcqaDC/vm/vm-operator"
	// underlying SOAP error is not typed, thus ugly grepping hack
	alreadyDeletedErr = "has already been deleted or has not been completely created"
	// max number parallel vCenter operations
	defaultConcurrency = 3
)

func getVMGroup(ctx context.Context, finder *find.Finder, vmgroup string) (*object.Folder, error) {
	path := vmPath + "/" + vmgroup
	f, err := finder.Folder(ctx, path)
	if err != nil {
		return nil, errors.Wrapf(err, "could not retrieve vm group %q", vmgroup)
	}

	return f, nil
}

func createVMGroup(ctx context.Context, finder *find.Finder, vmgroup string) (*object.Folder, error) {
	f, err := finder.Folder(ctx, vmPath)
	if err != nil {
		return nil, errors.Wrap(err, "could not get vm-operator folder")
	}

	group, err := f.CreateFolder(ctx, vmgroup)
	if err != nil {
		return nil, errors.Wrapf(err, "could not create vm group folder %q", vmgroup)
	}

	return group, nil
}

func getReplicas(ctx context.Context, finder *find.Finder, group string) ([]*object.VirtualMachine, error) {
	g, err := finder.Folder(ctx, vmPath+"/"+group)
	if err != nil {
		return nil, errors.Wrapf(err, "could not find vm group %q", group)
	}

	return finder.VirtualMachineList(ctx, g.InventoryPath+"/*")
}

func cloneVM(ctx context.Context, finder *find.Finder, template string, name string, destination string, pool *object.ResourcePool, spec v1alpha1.VmGroupSpec) error {
	tmpl, err := finder.VirtualMachine(ctx, template)
	if err != nil {
		return errors.Wrap(err, "could not find template")
	}

	folder, err := finder.Folder(ctx, destination)
	if err != nil {
		return errors.Wrap(err, "could not find destination folder")
	}

	rpRef := pool.Reference()
	cs := types.VirtualMachineCloneSpec{
		Location: types.VirtualMachineRelocateSpec{
			Pool: &rpRef,
		},
		Config: &types.VirtualMachineConfigSpec{
			NumCPUs:  spec.CPU,
			MemoryMB: int64(1024 * spec.Memory),
		},
		PowerOn: true,
	}

	task, err := tmpl.Clone(ctx, folder, name, cs)
	if err != nil {
		return errors.Wrap(err, "could not initiate clone task")
	}

	err = task.Wait(ctx)
	if err != nil {
		return errors.Wrapf(err, "could not create clone %q", name)
	}

	return nil
}

func isPoweredOn(ctx context.Context, vm *object.VirtualMachine) (bool, error) {
	p, err := vm.PowerState(ctx)
	if err != nil {
		return false, err
	}

	return p == types.VirtualMachinePowerStatePoweredOn, nil
}

func deleteVM(ctx context.Context, vm *object.VirtualMachine) error {
	task, _ := vm.PowerOff(ctx)

	// we don't care about any errors during power off
	_ = task.Wait(ctx)

	task, err := vm.Destroy(ctx)
	if err != nil {
		if strings.Contains(err.Error(), alreadyDeletedErr) {
			// already deleted
			return nil
		}
		return errors.Wrapf(err, "could not delete vm %q", vm.InventoryPath)
	}

	err = task.Wait(ctx)
	if err != nil {
		return errors.Wrap(err, "vm delete task failed")
	}
	return nil
}

func deleteFolder(ctx context.Context, group *object.Folder) error {
	task, err := group.Destroy(ctx)
	if err != nil {
		if strings.Contains(err.Error(), alreadyDeletedErr) {
			// already deleted
			return nil
		}
		return errors.Wrapf(err, "could not delete folder %q", group.InventoryPath)
	}

	err = task.Wait(ctx)
	if err != nil {
		return errors.Wrap(err, "folder delete task failed")
	}
	return nil
}
