package phases

import (
	"errors"
	"fmt"

	// initialize konfigadm

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"github.com/flanksource/commons/certs"
	_ "github.com/moshloop/konfigadm/pkg"
	konfigadm "github.com/moshloop/konfigadm/pkg/types"
	"github.com/moshloop/platform-cli/pkg/phases/kubeadm"
	"github.com/moshloop/platform-cli/pkg/platform"
)

var envVars = map[string]string{
	"ETCDCTL_ENDPOINTS": "https://127.0.0.1:2379",
	"ETCDCTL_CACERT":    "/etc/kubernetes/pki/etcd/ca.crt",
	"ETCDCTL_CERT":      "/etc/kubernetes/pki/etcd/healthcheck-client.crt",
	"ETCDCTL_KEY":       "/etc/kubernetes/pki/etcd/healthcheck-client.key",
	"KUBECONFIG":        "/etc/kubernetes/admin.conf",
}

func CreatePrimaryMaster(platform *platform.Platform) (*konfigadm.Config, error) {
	if platform.Name == "" {
		return nil, errors.New("Must specify a platform name")
	}
	if platform.Datacenter == "" {
		return nil, errors.New("Must specify a platform datacenter")
	}
	hostname := ""
	cfg, err := baseKonfig(platform)
	if err != nil {
		log.Tracef("CreatePrimaryMaster: Failed to get baseKonfig: %s", err)
		return nil, err
	}
	if err := addInitKubeadmConfig(hostname, platform, cfg); err != nil {
		log.Tracef("CreatePrimaryMaster: Failed to add kubeadm config: %s", err)
		return nil, err
	}
	createConsulService(hostname, platform, cfg)
	createClientSideLoadbalancers(platform, cfg)
	addCerts(platform, cfg)
	cfg.AddCommand("kubeadm init --config /etc/kubernetes/kubeadm.conf | tee /var/log/kubeadm.log")
	return cfg, nil
}

func baseKonfig(platform *platform.Platform) (*konfigadm.Config, error) {
	platform.Init()
	cfg, err := konfigadm.NewConfig().Build()
	if err != nil {
		log.Tracef("baseKonfig: Failed to get config: %s", err)
		return nil, err
	}
	for k, v := range envVars {
		cfg.Environment[k] = v
	}

	// update hosts file with hostname
	cfg.AddCommand("echo $(ifconfig ens160 | grep inet | awk '{print $2}' | head -n1 ) $(hostname) >> /etc/hosts")
	return cfg, nil
}

func addCerts(platform *platform.Platform, cfg *konfigadm.Config) error {
	clusterCA := certs.NewCertificateBuilder("kubernetes-ca").CA().Certificate
	clusterCA, err := platform.GetCA().SignCertificate(clusterCA, 10)
	if err != nil {
		log.Tracef("addCerts: Failed to sign certificate: %s", err)
		return err
	}

	// plus any cert signed by this cluster specific CA
	crt := string(clusterCA.EncodedCertificate()) + "\n"
	// any cert signed by the global CA should be allowed
	crt = crt + string(platform.GetCA().GetPublicChain()[0].EncodedCertificate()) + "\n"
	// csrsigning controller doesn't like having more than 1 CA cert passed to it
	cfg.Files["/etc/kubernetes/pki/csr-ca.crt"] = string(clusterCA.EncodedCertificate())
	cfg.Files["/etc/kubernetes/pki/csr-ca.key"] = string(clusterCA.EncodedPrivateKey())

	cfg.Files["/etc/kubernetes/pki/ca.crt"] = crt
	cfg.Files["/etc/kubernetes/pki/ca.key"] = string(clusterCA.EncodedPrivateKey())
	cfg.Files["/etc/ssl/certs/openid-ca.pem"] = string(platform.GetIngressCA().GetPublicChain()[0].EncodedCertificate())
	return nil
}

func addInitKubeadmConfig(hostname string, platform *platform.Platform, cfg *konfigadm.Config) error {
	cluster := kubeadm.NewClusterConfig(platform)
	data, err := yaml.Marshal(cluster)
	if err != nil {
		log.Tracef("addInitKubeadmConfig: Failed to marshal cluster config: %s", err)
		return err
	}
	log.Tracef("Using kubeadm config: \n%s", string(data))
	cfg.Files["/etc/kubernetes/kubeadm.conf"] = string(data)
	return nil
}

func createConsulService(hostname string, platform *platform.Platform, cfg *konfigadm.Config) {
	cfg.Files["/etc/kubernetes/consul/api.json"] = fmt.Sprintf(`
{
	"leave_on_terminate": true,
  "rejoin_after_leave": true,
	"service": {
		"id": "%s",
		"name": "%s",
		"address": "",
		"check": {
			"id": "api-server",
			"name": " TCP on port 6443",
			"tcp": "localhost:6443",
			"interval": "120s",
			"timeout": "60s"
		},
		"port": 6443,
		"enable_tag_override": false
	}
}
	`, hostname, platform.Name)
}

func createClientSideLoadbalancers(platform *platform.Platform, cfg *konfigadm.Config) {
	cfg.Containers = append(cfg.Containers, konfigadm.Container{
		Image: platform.GetImagePath("docker.io/consul:1.3.1"),
		Env: map[string]string{
			"CONSUL_CLIENT_INTERFACE": "ens160",
			"CONSUL_BIND_INTERFACE":   "ens160",
		},
		Args:       fmt.Sprintf("agent -join=%s:8301 -datacenter=%s -data-dir=/consul/data -domain=consul -config-dir=/consul-configs", platform.Consul, platform.Datacenter),
		DockerOpts: "--net host",
		Volumes: []string{
			"/etc/kubernetes/consul:/consul-configs",
		},
	}, konfigadm.Container{
		Image:      platform.GetImagePath("docker.io/moshloop/tcp-loadbalancer:0.1"),
		Service:    "haproxy",
		DockerOpts: "--net host -p 8443:8443",
		Env: map[string]string{
			"CONSUL_CONNECT": platform.Consul + ":8500",
			"SERVICE_NAME":   platform.Name,
			"PORT":           "8443",
		},
	})
}

func CreateSecondaryMaster(platform *platform.Platform) (*konfigadm.Config, error) {
	hostname := ""
	cfg, err := baseKonfig(platform)
	if err != nil {
		log.Tracef("CreateSecondaryMaster: Failed to get baseKonfig: %s", err)
		return nil, err
	}
	token, err := kubeadm.GetOrCreateBootstrapToken(platform)
	if err != nil {
		log.Tracef("CreateSecondaryMaster: Failed to get/create bootstrap token: %s", err)
		return nil, err
	}
	certKey, err := kubeadm.UploadControlPaneCerts(platform)
	if err != nil {
		log.Tracef("CreateSecondaryMaster: Failed to upload control plane certs: %s", err)
		return nil, err
	}
	createConsulService(hostname, platform, cfg)
	createClientSideLoadbalancers(platform, cfg)
	addCerts(platform, cfg)
	cfg.AddCommand(fmt.Sprintf(
		"kubeadm join --control-plane --token %s --certificate-key %s --discovery-token-unsafe-skip-ca-verification %s  | tee /var/log/kubeadm.log",
		token, certKey, platform.JoinEndpoint))
	return cfg, nil
}

func CreateWorker(platform *platform.Platform) (*konfigadm.Config, error) {
	cfg, err := baseKonfig(platform)
	if err != nil {
		log.Tracef("CreateWorker: Failed to get baseKonfig: %s", err)
		return nil, err
	}
	token, err := kubeadm.GetOrCreateBootstrapToken(platform)
	if err != nil {
		log.Tracef("CreateWorker: Failed to get/create bootstrap token: %s", err)
		return nil, err
	}
	createClientSideLoadbalancers(platform, cfg)
	cfg.AddCommand(fmt.Sprintf(
		"kubeadm join --token %s --discovery-token-unsafe-skip-ca-verification %s  | tee /var/log/kubeadm.log",
		token, platform.JoinEndpoint))
	return cfg, nil
}
