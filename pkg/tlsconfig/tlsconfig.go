// Package tlsconfig converts OpenShift TLS security profiles
// (configv1.TLSSecurityProfile) into Go crypto/tls configuration using
// library-go shared utilities, following the pattern from openshift/hive#2800.
package tlsconfig

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

	libgocrypto "github.com/openshift/library-go/pkg/crypto"
	"github.com/openshift/library-go/pkg/operator/configobserver/apiserver"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("tlsconfig")

const (
	// observedConfigKeyServingInfo is the top-level key in the observed config
	// returned by library-go's ObserveTLSSecurityProfile.
	observedConfigKeyServingInfo = "servingInfo"
	// observedConfigKeyMinTLSVersion is the key for the minimum TLS version.
	observedConfigKeyMinTLSVersion = "minTLSVersion"
	// observedConfigKeyCipherSuites is the key for the cipher suites list.
	observedConfigKeyCipherSuites = "cipherSuites"

	// recorderComponentName identifies this package in library-go event recordings.
	recorderComponentName = "tlsconfig"
)

// TODO: Once openshift/api is bumped to a version that includes the Groups
// field on TLSProfileSpec (added upstream for OCP 4.20), wire up
// tls.Config.CurvePreferences from the profile's Groups to support
// post-quantum key agreement (e.g. X25519MLKEM768).

// TLSConfigFromCluster uses library-go's TLS security profile observer to
// read the cluster's APIServer configuration and returns a function that
// configures a tls.Config accordingly. If the APIServer resource is not
// found, the Intermediate profile is used as default.
func TLSConfigFromCluster(ctx context.Context, reader client.Reader) (func(*tls.Config), error) {
	listers := &observerListers{
		lister: &directAPIServerLister{ctx: ctx, reader: reader},
	}
	recorder := events.NewInMemoryRecorder(recorderComponentName)

	observedConfig, errs := apiserver.ObserveTLSSecurityProfile(listers, recorder, map[string]interface{}{})
	if len(errs) > 0 {
		return nil, fmt.Errorf("observing TLS security profile: %w", errors.Join(errs...))
	}

	minVersionStr, _, err := unstructured.NestedString(observedConfig, observedConfigKeyServingInfo, observedConfigKeyMinTLSVersion)
	if err != nil {
		return nil, fmt.Errorf("extracting minTLSVersion from observed config: %w", err)
	}
	cipherNames, _, err := unstructured.NestedStringSlice(observedConfig, observedConfigKeyServingInfo, observedConfigKeyCipherSuites)
	if err != nil {
		return nil, fmt.Errorf("extracting cipherSuites from observed config: %w", err)
	}

	return tlsConfigFunc(minVersionStr, cipherNames)
}

// tlsConfigFunc converts a TLS version string and IANA cipher suite names
// (as produced by library-go's observer) into a tls.Config modifier function.
func tlsConfigFunc(minVersionStr string, cipherNames []string) (func(*tls.Config), error) {
	minVersion, err := libgocrypto.TLSVersion(minVersionStr)
	if err != nil {
		return nil, fmt.Errorf("parsing TLS version %q: %w", minVersionStr, err)
	}

	var cipherSuites []uint16
	for _, name := range cipherNames {
		id, cipherErr := libgocrypto.CipherSuite(name)
		if cipherErr != nil {
			log.Info("Skipping cipher suite not supported by Go's crypto/tls", "cipher", name)
			continue
		}
		cipherSuites = append(cipherSuites, id)
	}

	return func(cfg *tls.Config) {
		cfg.MinVersion = minVersion
		if len(cipherSuites) > 0 {
			cfg.CipherSuites = cipherSuites
		}
	}, nil
}
