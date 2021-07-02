package kubevip

import (
	"github.com/flanksource/karina/pkg/constants"
	"github.com/flanksource/karina/pkg/platform"
)

const (
	Namespace = constants.PlatformSystem
	Name      = "kube-vip"
)

// Deploy deploys the konfig-manager into the platform-system namespace
func Deploy(p *platform.Platform) error {

	if err := p.CreateOrUpdateNamespace(Namespace, nil, nil); err != nil {
		return err
	}
	return p.ApplySpecs(Namespace, "kube-vip.yaml")
}