package phases

import (
	"io/ioutil"
	"os"

	"github.com/flanksource/commons/deps"
	"github.com/flanksource/commons/files"
	"github.com/flanksource/commons/is"
	"github.com/moshloop/platform-cli/pkg/types"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

func Build(cfg types.PlatformConfig) error {
	tmp, _ := ioutil.TempFile("", "config*.yml")
	data, _ := yaml.Marshal(cfg)
	tmp.WriteString(string(data))
	os.Mkdir("build", 0755)
	gomplate := deps.Binary("gomplate", cfg.Versions["gomplate"], ".bin")
	kustomize := deps.Binary("kustomize", cfg.Versions["kustomize"], ".bin")

	for k, url := range cfg.Resources {
		if !is.File(k) {
			if err := files.Getter(url, k); err != nil {
				log.Tracef("Build: Failed get url: %s", err)
				return err
			}
		}
	}

	for _, spec := range cfg.Specs {
		log.Infof("Building specs in %s", spec)
		absPath, _ := os.Readlink(spec)
		if err := gomplate("--input-dir \"%s\" --output-dir build -c \".=%s\"", absPath, tmp.Name()); err != nil {
			log.Tracef("Build: Failed to template: %s", err)
			return err
		}
	}

	if files.Exists("kustomization.yaml") {
		log.Infoln("Building with kustomize")
		os.Remove("build/kustomize.yml")
		if err := kustomize("build > build/kustomize.yml"); err != nil {
			log.Tracef("Build: Failed to apply kustomize: %s", err)
			return err
		}
	}

	return nil
}
