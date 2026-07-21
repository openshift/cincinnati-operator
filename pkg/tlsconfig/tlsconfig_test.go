package tlsconfig

import (
	"context"
	"crypto/tls"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestTLSConfigFromCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, configv1.Install(scheme))

	tests := []struct {
		name               string
		apiServer          *configv1.APIServer
		wantMinVersion     uint16
		wantCipherSuiteIDs []uint16
	}{
		{
			name: "nil TLS profile defaults to Intermediate",
			apiServer: &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			},
			wantMinVersion: tls.VersionTLS12,
			wantCipherSuiteIDs: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			},
		},
		{
			name: "Modern profile sets TLS 1.3 with no TLS 1.2 ciphers",
			apiServer: &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type:   configv1.TLSProfileModernType,
						Modern: &configv1.ModernTLSProfile{},
					},
				},
			},
			wantMinVersion:     tls.VersionTLS13,
			wantCipherSuiteIDs: nil,
		},
		{
			name: "Old profile sets TLS 1.0 with all supported ciphers",
			apiServer: &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileOldType,
						Old:  &configv1.OldTLSProfile{},
					},
				},
			},
			wantMinVersion: tls.VersionTLS10,
		},
		{
			name: "Custom profile with specific ciphers",
			apiServer: &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileCustomType,
						Custom: &configv1.CustomTLSProfile{
							TLSProfileSpec: configv1.TLSProfileSpec{
								Ciphers: []string{
									"ECDHE-ECDSA-AES128-GCM-SHA256",
									"ECDHE-RSA-CHACHA20-POLY1305",
								},
								MinTLSVersion: configv1.VersionTLS12,
							},
						},
					},
				},
			},
			wantMinVersion: tls.VersionTLS12,
			wantCipherSuiteIDs: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			},
		},
		{
			name: "unsupported ciphers are silently skipped",
			apiServer: &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileCustomType,
						Custom: &configv1.CustomTLSProfile{
							TLSProfileSpec: configv1.TLSProfileSpec{
								Ciphers: []string{
									"DHE-RSA-AES128-GCM-SHA256",   // unsupported by Go
									"ECDHE-RSA-AES128-GCM-SHA256", // supported
									"SOME-UNKNOWN-CIPHER",         // unknown
								},
								MinTLSVersion: configv1.VersionTLS12,
							},
						},
					},
				},
			},
			wantMinVersion: tls.VersionTLS12,
			wantCipherSuiteIDs: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.apiServer).
				Build()

			tlsOpt, err := TLSConfigFromCluster(context.Background(), reader)
			require.NoError(t, err)

			cfg := &tls.Config{}
			tlsOpt(cfg)

			assert.Equal(t, tt.wantMinVersion, cfg.MinVersion)

			if tt.wantCipherSuiteIDs != nil {
				assert.Equal(t, tt.wantCipherSuiteIDs, cfg.CipherSuites)
			}

			if tt.name == "Old profile sets TLS 1.0 with all supported ciphers" {
				assert.Greater(t, len(cfg.CipherSuites), 10)
			}
		})
	}
}

func TestTLSConfigFromClusterNoAPIServer(t *testing.T) {
	// When the APIServer resource doesn't exist, the observer falls back
	// to the Intermediate profile.
	scheme := runtime.NewScheme()
	require.NoError(t, configv1.Install(scheme))

	reader := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	tlsOpt, err := TLSConfigFromCluster(context.Background(), reader)
	require.NoError(t, err)

	cfg := &tls.Config{}
	tlsOpt(cfg)

	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	assert.NotEmpty(t, cfg.CipherSuites)
}

func TestTLSConfigFunc(t *testing.T) {
	tests := []struct {
		name           string
		minVersion     string
		ciphers        []string
		wantMinVersion uint16
		wantCipherIDs  []uint16
		wantErr        bool
	}{
		{
			name:       "valid version and ciphers",
			minVersion: "VersionTLS12",
			ciphers: []string{
				"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
				"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			},
			wantMinVersion: tls.VersionTLS12,
			wantCipherIDs: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			},
		},
		{
			name:           "TLS 1.3",
			minVersion:     "VersionTLS13",
			ciphers:        []string{},
			wantMinVersion: tls.VersionTLS13,
		},
		{
			name:       "invalid version returns error",
			minVersion: "VersionTLS99",
			ciphers:    []string{},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlsOpt, err := tlsConfigFunc(tt.minVersion, tt.ciphers)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			cfg := &tls.Config{}
			tlsOpt(cfg)

			assert.Equal(t, tt.wantMinVersion, cfg.MinVersion)
			if tt.wantCipherIDs != nil {
				assert.Equal(t, tt.wantCipherIDs, cfg.CipherSuites)
			}
		})
	}
}
