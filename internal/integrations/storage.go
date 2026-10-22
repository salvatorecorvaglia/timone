package integrations

import (
	"fmt"
	"k8s.io/apimachinery/pkg/util/validation"
	"strconv"
	"timone/internal/model"
)

// HelmStorageIdentity validates the same metadata for every Helm consumer.
// Labels remain an association, not a cryptographic ownership assertion.
func HelmStorageIdentity(o model.Object) (string, int, bool) {
	name := model.Str(o, "metadata", "labels", "name")
	ns := model.Str(o, "metadata", "namespace")
	rev, err := strconv.Atoi(model.Str(o, "metadata", "labels", "version"))
	valid := err == nil && rev > 0 && rev <= 2147483647 && len(name) <= 53 && len(validation.IsDNS1123Subdomain(name)) == 0 && len(validation.IsDNS1123Label(ns)) == 0 && model.Str(o, "type") == "helm.sh/release.v1" && model.Str(o, "metadata", "labels", "owner") == "helm" && model.Str(o, "metadata", "name") == fmt.Sprintf("sh.helm.release.v1.%s.v%d", name, rev)
	return name, rev, valid
}
