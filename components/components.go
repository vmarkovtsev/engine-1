package components

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/src-d/engine/docker"
)

var srcdNamespaces = []string{
	"srcd",
	"bblfsh",
	"pilosa",
}

type Component struct {
	Name    string
	Image   string
	Version string // only if there's a required version
}

const (
	BblfshVolume = "srcd-cli-bblfsh-storage"
)

var (
	Gitbase = Component{
		Name:  "srcd-cli-gitbase",
		Image: "srcd/gitbase",
	}

	GitbaseWeb = Component{
		Name:  "srcd-cli-gitbase-web",
		Image: "srcd/gitbase-web",
	}

	Bblfshd = Component{
		Name:  "srcd-cli-bblfshd",
		Image: "bblfsh/bblfshd",
	}

	BblfshWeb = Component{
		Name:  "srcd-cli-bblfsh-web",
		Image: "bblfsh/web",
	}

	Pilosa = Component{
		Name:    "srcd-cli-pilosa",
		Image:   "pilosa/pilosa",
		Version: "v0.9.0",
	}

	workDirDependants = []Component{
		Gitbase,
		Pilosa,
		Bblfshd, // does not depend on workdir but it does depend on user dir
	}
)

type FilterFunc func(string) bool

func filter(cmps []string, filters []FilterFunc) []string {
	var result []string
	for _, cmp := range cmps {
		var add = true
		for _, f := range filters {
			if !f(cmp) {
				add = false
				break
			}
		}

		if add {
			result = append(result, cmp)
		}
	}
	return result
}

func IsWorkingDirDependant(cmp string) bool {
	for _, c := range workDirDependants {
		if c.Name == cmp {
			return true
		}
	}
	return false
}

func List(ctx context.Context, filters ...FilterFunc) ([]string, error) {
	c, err := client.NewEnvClient()
	if err != nil {
		return nil, err
	}

	imgs, err := c.ImageList(ctx, types.ImageListOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not list components: %v", err)
	}

	var res []string
	for _, img := range imgs {
		if len(img.RepoTags) == 0 {
			continue
		}

		if isSrcdComponent(img.RepoTags[0]) {
			res = append(res, img.RepoTags[0])
		}
	}

	if len(filters) > 0 {
		return filter(res, filters), nil
	}

	return res, nil
}

var ErrNotSrcd = fmt.Errorf("not srcd component")

// Install installs a new component.
func Install(ctx context.Context, id string) error {
	if !isSrcdComponent(id) {
		return ErrNotSrcd
	}

	image, version := splitImageID(id)
	return docker.Pull(ctx, image, version)
}

func IsInstalled(ctx context.Context, id string) (bool, error) {
	if !isSrcdComponent(id) {
		return false, ErrNotSrcd
	}

	image, version := splitImageID(id)
	return docker.IsInstalled(ctx, image, version)
}

func Purge() error {
	logrus.Info("removing containers...")
	if err := removeContainers(); err != nil {
		return errors.Wrap(err, "unable to remove all containers")
	}

	logrus.Info("removing volumes...")

	if err := removeVolumes(); err != nil {
		return errors.Wrap(err, "unable to remove volumes")
	}

	logrus.Info("removing images...")

	if err := removeImages(); err != nil {
		return errors.Wrap(err, "unable to remove all images")
	}

	return nil
}

func removeContainers() error {
	cs, err := docker.List()
	if err != nil {
		return err
	}

	for _, c := range cs {
		if len(c.Names) == 0 {
			continue
		}

		name := strings.TrimLeft(c.Names[0], "/")
		if isFromEngine(name) {
			logrus.Infof("removing container %s", name)

			if err := docker.Kill(name); err != nil {
				return err
			}
		}
	}

	return nil
}

func removeVolumes() error {
	vols, err := docker.ListVolumes(context.Background())
	if err != nil {
		return err
	}

	for _, vol := range vols {
		if isFromEngine(vol.Name) {
			logrus.Infof("removing volume %s", vol.Name)

			if err := docker.RemoveVolume(context.Background(), vol.Name); err != nil {
				return err
			}
		}
	}

	return nil
}

func removeImages() error {
	cmps, err := List(context.Background())
	if err != nil {
		return errors.Wrap(err, "unable to list images")
	}

	for _, cmp := range cmps {
		logrus.Infof("removing image %s", cmp)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()
		if err := docker.RemoveImage(ctx, cmp); err != nil {
			return err
		}
	}

	return nil
}

func splitImageID(id string) (image, version string) {
	parts := strings.Split(id, ":")
	image = parts[0]
	version = "latest"
	if len(parts) > 1 {
		version = parts[1]
	}
	return
}

func stringInSlice(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

func isSrcdComponent(id string) bool {
	namespace := strings.Split(id, "/")[0]
	return stringInSlice(srcdNamespaces, namespace)
}

func isFromEngine(name string) bool {
	return strings.HasPrefix(name, "srcd-cli-")
}
