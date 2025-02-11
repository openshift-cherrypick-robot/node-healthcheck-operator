package mhc

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/medik8s/node-healthcheck-operator/controllers/utils"
	"github.com/openshift/api/machine/v1beta1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// NodeConditionTerminating is the node condition type used by the termination handler MHC
const NodeConditionTerminating = "Terminating"

// Checker provides functions for checking for conflicts with MachineHealthCheck
type Checker interface {
	Start(context.Context) error
	UpdateStatus() error
	NeedDisableNHC() bool
	NeedIgnoreNode(*v1.Node) bool
}

// NewMHCChecker creates a new Checker
func NewMHCChecker(mgr manager.Manager) (Checker, error) {

	openshift, err := utils.IsOnOpenshift(mgr.GetConfig())
	if err != nil {
		return nil, err
	}
	if !openshift {
		return DummyChecker{}, nil
	}

	c := &checker{
		client:    mgr.GetClient(),
		logger:    mgr.GetLogger().WithName("MHCChecker"),
		mhcStatus: unknown,
	}
	return c, nil
}

type mhcStatus int

const (
	unknown mhcStatus = iota
	noMHC
	terminationMHCOnly
	customMHC
)

type checker struct {
	client     client.Client
	logger     logr.Logger
	mhcStatus  mhcStatus
	mhcRunning bool
}

var _ Checker = &checker{}

// Start will start the component and update the initial status
func (c *checker) Start(ctx context.Context) error {
	if err := c.UpdateStatus(); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
	}
	return nil
}

func (c *checker) UpdateStatus() error {
	mhcList := &v1beta1.MachineHealthCheckList{}
	if err := c.client.List(context.Background(), mhcList); err != nil {
		c.logger.Error(err, "failed to list MHC")
		return err
	}

	if len(mhcList.Items) == 0 {
		// no MHC found, we are fine
		if c.mhcStatus != noMHC {
			c.logger.Info("no MHC found")
		}
		c.mhcStatus = noMHC
		return nil
	} else if len(mhcList.Items) > 1 {
		// multiple MHCs found, disable NHC
		// log once only
		if c.mhcStatus != customMHC {
			c.logger.Info("found custom MHC, will disable NHC")
		}
		c.mhcStatus = customMHC
		return nil
	}

	// Only the one MHC which targets nodes with only Terminating condition is fine
	// NHC will ignore those nodes
	mhc := mhcList.Items[0]
	if len(mhc.Spec.UnhealthyConditions) == 1 && mhc.Spec.UnhealthyConditions[0].Type == NodeConditionTerminating {
		// log once only
		if c.mhcStatus != terminationMHCOnly {
			c.logger.Info("found termination handler MHC, will ignore Nodes with Terminating condition")
			c.mhcStatus = terminationMHCOnly
		}
		return nil
	}

	// Everything else might cause conflicts
	// log once only
	if c.mhcStatus != customMHC {
		c.logger.Info("found custom MHC, will disable NHC")
	}
	c.mhcStatus = customMHC
	return nil

}

// NeedDisableNHC checks if NHC needs to be disabled, because custom MHCs are configured in the cluster,
// in order to avoid conflicts
func (c *checker) NeedDisableNHC() bool {
	switch c.mhcStatus {
	case unknown, noMHC, terminationMHCOnly:
		return false
	case customMHC:
		return true
	default:
		return false
	}
}

// NeedIgnoreNode checks if remediation of a certain node needs to be ignored, because it is handled the default
// termination handler MHC, see https://github.com/openshift/enhancements/blob/master/enhancements/machine-api/spot-instances.md
func (c *checker) NeedIgnoreNode(node *v1.Node) bool {

	// if no MHC configured, don't ignore any node
	if c.mhcStatus == noMHC {
		return false
	}

	// ignore node with condition "Terminating"
	for _, cond := range node.Status.Conditions {
		if cond.Type == NodeConditionTerminating {
			c.logger.Info("ignoring unhealthy Node, it is terminating and will be handled by MHC", "NodeName", node.GetName())
			return true
		}
	}

	return false
}

// DummyChecker can be used in non Openshift clusters or in tests
// Using NewMHCChecker is recommended though
type DummyChecker struct{}

var _ Checker = DummyChecker{}

// Start will start the component, no op on non openshift clusters
func (d DummyChecker) Start(ctx context.Context) error {
	select {
	case <-ctx.Done():
	}
	return nil
}

// UpdateStatus always return no error on non openshift clusters
func (d DummyChecker) UpdateStatus() error {
	return nil
}

// NeedDisableNHC always return false on non openshift clusters
func (d DummyChecker) NeedDisableNHC() bool {
	return false
}

// NeedIgnoreNode always return false on non openshift clusters
func (d DummyChecker) NeedIgnoreNode(node *v1.Node) bool {
	return false
}
