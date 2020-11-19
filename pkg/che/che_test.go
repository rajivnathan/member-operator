package che

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/codeready-toolchain/toolchain-common/pkg/test"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUserExists(t *testing.T) {
	// given
	testCheURL := "https://codeready-codeready-workspaces-operator.member-cluster"
	testSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "toolchain-member",
		},
		Data: map[string][]byte{
			"che.admin.username": []byte("test-che-user"),
			"che.admin.password": []byte("test-che-password"),
		},
	}

	restore := test.SetEnvVarsAndRestore(t,
		test.Env("WATCH_NAMESPACE", "toolchain-member"),
		test.Env("MEMBER_OPERATOR_SECRET_NAME", "test-secret"),
	)
	defer restore()

	t.Run("missing che route", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, testSecret, keycloackRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: tokenCacheWithValidToken(),
		}

		// when
		exists, err := cheClient.UserExists("test-user")

		// then
		require.EqualError(t, err, `request to find Che user 'test-user' failed: routes.route.openshift.io "codeready" not found`)
		require.False(t, exists)
	})

	t.Run("unexpected error", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, testSecret, cheRoute(true), keycloackRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: tokenCacheWithValidToken(),
		}

		// when
		defer gock.OffAll()
		gock.New(testCheURL).
			Get(cheUserFindPath).
			MatchHeader("Authorization", "Bearer abc.123.xyz").
			Persist().
			Reply(400).
			BodyString(`{"error":"che error"}`)
		exists, err := cheClient.UserExists("test-user")

		// then
		require.EqualError(t, err, `request to find Che user 'test-user' failed, Response status: '400 Bad Request'`)
		require.False(t, exists)
	})

	t.Run("user not found", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, testSecret, cheRoute(true), keycloackRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: tokenCacheWithValidToken(),
		}

		// when
		defer gock.OffAll()
		gock.New(testCheURL).
			Get(cheUserFindPath).
			MatchHeader("Authorization", "Bearer abc.123.xyz").
			Persist().
			Reply(404).
			BodyString(`{"error":"che error"}`)
		exists, err := cheClient.UserExists("test-user")

		// then
		require.NoError(t, err)
		require.False(t, exists)
	})

	t.Run("user found", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, testSecret, cheRoute(true), keycloackRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: tokenCacheWithValidToken(),
		}

		// when
		defer gock.OffAll()
		gock.New(testCheURL).
			Get(cheUserFindPath).
			MatchHeader("Authorization", "Bearer abc.123.xyz").
			Persist().
			Reply(200).
			BodyString(`{"userID":"abc123"}`)
		exists, err := cheClient.UserExists("test-user")

		// then
		require.NoError(t, err)
		require.True(t, exists)
	})
}

func TestGetUserIDByUsername(t *testing.T) {
	// given
	testCheURL := "https://codeready-codeready-workspaces-operator.member-cluster"
	testSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "toolchain-member",
		},
		Data: map[string][]byte{
			"che.admin.username": []byte("test-che-user"),
			"che.admin.password": []byte("test-che-password"),
		},
	}

	restore := test.SetEnvVarsAndRestore(t,
		test.Env("WATCH_NAMESPACE", "toolchain-member"),
		test.Env("MEMBER_OPERATOR_SECRET_NAME", "test-secret"),
	)
	defer restore()

	t.Run("missing che route", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, testSecret, keycloackRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: tokenCacheWithValidToken(),
		}

		// when
		userID, err := cheClient.GetUserIDByUsername("test-user")

		// then
		require.EqualError(t, err, `unable to get Che user ID for user 'test-user': routes.route.openshift.io "codeready" not found`)
		require.Empty(t, userID)
	})

	t.Run("unexpected error", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, testSecret, cheRoute(true), keycloackRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: tokenCacheWithValidToken(),
		}

		// when
		defer gock.OffAll()
		gock.New(testCheURL).
			Get(cheUserFindPath).
			MatchHeader("Authorization", "Bearer abc.123.xyz").
			Persist().
			Reply(400).
			BodyString(`{"error":"che error"}`)
		userID, err := cheClient.GetUserIDByUsername("test-user")

		// then
		require.EqualError(t, err, `unable to get Che user ID for user 'test-user', Response status: '400 Bad Request' Body: '{"error":"che error"}'`)
		require.Empty(t, userID)
	})

	t.Run("user ID parse error", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, testSecret, cheRoute(true), keycloackRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: tokenCacheWithValidToken(),
		}

		// when
		defer gock.OffAll()
		gock.New(testCheURL).
			Get(cheUserFindPath).
			MatchHeader("Authorization", "Bearer abc.123.xyz").
			Persist().
			Reply(200)
		userID, err := cheClient.GetUserIDByUsername("test-user")

		// then
		require.EqualError(t, err, `unable to get Che user ID for user 'test-user': error unmarshalling Che user json  : unexpected end of JSON input`)
		require.Empty(t, userID)
	})

	t.Run("bad body", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, testSecret, cheRoute(true), keycloackRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: tokenCacheWithValidToken(),
		}

		// when
		defer gock.OffAll()
		gock.New(testCheURL).
			Get(cheUserFindPath).
			MatchHeader("Authorization", "Bearer abc.123.xyz").
			Persist().
			Reply(200).
			BodyString(`{"name":"test-user"}`)
		userID, err := cheClient.GetUserIDByUsername("test-user")

		// then
		require.EqualError(t, err, `unable to get Che user ID for user 'test-user': unable to get che user information: Body: '{"name":"test-user"}'`)
		require.Empty(t, userID)
	})

	t.Run("success", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, testSecret, cheRoute(true), keycloackRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: tokenCacheWithValidToken(),
		}

		// when
		defer gock.OffAll()
		gock.New(testCheURL).
			Get(cheUserFindPath).
			MatchHeader("Authorization", "Bearer abc.123.xyz").
			Persist().
			Reply(200).
			BodyString(`{"name":"test-user","id":"abc1234"}`)
		userID, err := cheClient.GetUserIDByUsername("test-user")

		// then
		require.NoError(t, err)
		require.Equal(t, "abc1234", userID)
	})
}

func TestDeleteUser(t *testing.T) {
	// given
	testCheURL := "https://codeready-codeready-workspaces-operator.member-cluster"
	testSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "toolchain-member",
		},
		Data: map[string][]byte{
			"che.admin.username": []byte("test-che-user"),
			"che.admin.password": []byte("test-che-password"),
		},
	}

	restore := test.SetEnvVarsAndRestore(t,
		test.Env("WATCH_NAMESPACE", "toolchain-member"),
		test.Env("MEMBER_OPERATOR_SECRET_NAME", "test-secret"),
	)
	defer restore()

	t.Run("missing che route", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, testSecret, keycloackRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: tokenCacheWithValidToken(),
		}

		// when
		err := cheClient.DeleteUser("asdf-hjkl")

		// then
		require.EqualError(t, err, `unable to delete Che user with ID 'asdf-hjkl': routes.route.openshift.io "codeready" not found`)
	})

	t.Run("unexpected error", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, testSecret, cheRoute(true), keycloackRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: tokenCacheWithValidToken(),
		}

		// when
		defer gock.OffAll()
		gock.New(testCheURL).
			Delete(cheUserPath).
			MatchHeader("Authorization", "Bearer abc.123.xyz").
			Persist().
			Reply(400).
			BodyString(`{"error":"che error"}`)
		err := cheClient.DeleteUser("asdf-hjkl")

		// then
		require.EqualError(t, err, `unable to delete Che user with ID 'asdf-hjkl', Response status: '400 Bad Request' Body: '{"error":"che error"}'`)
	})

	t.Run("user not found", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, testSecret, cheRoute(true), keycloackRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: tokenCacheWithValidToken(),
		}

		// when
		defer gock.OffAll()
		gock.New(testCheURL).
			Delete(cheUserPath).
			MatchHeader("Authorization", "Bearer abc.123.xyz").
			Persist().
			Reply(404).
			BodyString(`{"message":"user not found"}`)
		err := cheClient.DeleteUser("asdf-hjkl")

		// then
		require.NoError(t, err)
	})

	t.Run("success", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, testSecret, cheRoute(true), keycloackRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: tokenCacheWithValidToken(),
		}

		// when
		defer gock.OffAll()
		gock.New(testCheURL).
			Delete(cheUserPath).
			MatchHeader("Authorization", "Bearer abc.123.xyz").
			Persist().
			Reply(204)
		err := cheClient.DeleteUser("asdf-hjkl")

		// then
		require.NoError(t, err)
	})
}

func TestCheRequest(t *testing.T) {
	// given
	testCheURL := "https://codeready-codeready-workspaces-operator.member-cluster"
	testSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "toolchain-member",
		},
		Data: map[string][]byte{
			"che.admin.username": []byte("test-che-user"),
			"che.admin.password": []byte("test-che-password"),
		},
	}

	t.Run("missing configuration", func(t *testing.T) {
		// given
		cl, cfg := prepareClientAndConfig(t, cheRoute(true))
		cheClient := &Client{
			config:     cfg,
			httpClient: http.DefaultClient,
			k8sClient:  cl,
			tokenCache: &tokenCache{
				httpClient: http.DefaultClient,
			},
		}

		// when
		res, err := cheClient.cheRequest(http.MethodGet, "", url.Values{})

		// then
		require.EqualError(t, err, "the che admin username and/or password are not configured")
		require.Nil(t, res)
	})

	t.Run("error scenarios", func(t *testing.T) {

		restore := test.SetEnvVarsAndRestore(t,
			test.Env("WATCH_NAMESPACE", "toolchain-member"),
			test.Env("MEMBER_OPERATOR_SECRET_NAME", "test-secret"),
		)
		defer restore()

		t.Run("no che route", func(t *testing.T) {
			// given
			cl, cfg := prepareClientAndConfig(t, testSecret)
			cheClient := &Client{
				config:     cfg,
				httpClient: http.DefaultClient,
				k8sClient:  cl,
				tokenCache: &tokenCache{
					httpClient: http.DefaultClient,
				},
			}

			// when
			res, err := cheClient.cheRequest(http.MethodGet, "", url.Values{})

			// then
			require.EqualError(t, err, `routes.route.openshift.io "codeready" not found`)
			require.Nil(t, res)
		})

		t.Run("no keycloak route", func(t *testing.T) {
			// given
			cl, cfg := prepareClientAndConfig(t, testSecret, cheRoute(true))
			cheClient := &Client{
				config:     cfg,
				httpClient: http.DefaultClient,
				k8sClient:  cl,
				tokenCache: &tokenCache{
					httpClient: http.DefaultClient,
				},
			}

			// when
			res, err := cheClient.cheRequest(http.MethodGet, "", url.Values{})

			// then
			require.EqualError(t, err, `routes.route.openshift.io "keycloak" not found`)
			require.Nil(t, res)
		})

		t.Run("no query params", func(t *testing.T) {
			// given
			cl, cfg := prepareClientAndConfig(t, testSecret, cheRoute(true))
			cheClient := &Client{
				config:     cfg,
				httpClient: http.DefaultClient,
				k8sClient:  cl,
				tokenCache: &tokenCache{
					httpClient: http.DefaultClient,
				},
			}

			// when
			res, err := cheClient.cheRequest(http.MethodGet, "", url.Values{})

			// then
			require.EqualError(t, err, `routes.route.openshift.io "keycloak" not found`)
			require.Nil(t, res)
		})

		t.Run("che returns error", func(t *testing.T) {
			// given
			cl, cfg := prepareClientAndConfig(t, testSecret, cheRoute(true), keycloackRoute(true))
			cheClient := &Client{
				config:     cfg,
				httpClient: http.DefaultClient,
				k8sClient:  cl,
				tokenCache: tokenCacheWithValidToken(),
			}

			// when
			defer gock.OffAll()
			gock.New(testCheURL).
				Get(cheUserFindPath).
				MatchHeader("Authorization", "Bearer abc.123.xyz").
				Persist().
				Reply(400).
				BodyString(`{"error":"che error"}`)
			res, err := cheClient.cheRequest(http.MethodGet, cheUserFindPath, url.Values{})

			// then
			require.NoError(t, err)
			require.Equal(t, 400, res.StatusCode)
		})
	})
}

func tokenCacheWithValidToken() *tokenCache {
	return &tokenCache{
		httpClient: http.DefaultClient,
		token: &TokenSet{
			AccessToken:  "abc.123.xyz",
			Expiration:   time.Now().Add(99 * time.Hour).Unix(),
			ExpiresIn:    99,
			RefreshToken: "111.222.333",
			TokenType:    "bearer",
		},
	}
}

// func prepareKeycloakMockResponse() {
// 	gock.New(keycloakURL).
// 		Post(tokenPath).
// 		MatchHeader("Content-Type", "application/x-www-form-urlencoded").
// 		Persist().
// 		Reply(200).
// 		BodyString(`{
// 					"access_token":"aaa.bbb.ccc",
// 					"expires_in":300,
// 					"refresh_expires_in":1800,
// 					"refresh_token":"111.222.333",
// 					"token_type":"bearer",
// 					"not-before-policy":0,
// 					"session_state":"a2fa1448-687a-414f-af40-3b6b3f5a873a",
// 					"scope":"profile email"
// 					}`)
// }

func cheRoute(tls bool) *routev1.Route {
	r := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "codeready",
			Namespace: "codeready-workspaces-operator",
		},
		Spec: routev1.RouteSpec{
			Host: fmt.Sprintf("codeready-codeready-workspaces-operator.%s", test.MemberClusterName),
			Path: "",
		},
	}
	if tls {
		r.Spec.TLS = &routev1.TLSConfig{
			Termination: "edge",
		}
	}
	return r
}
