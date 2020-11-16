package che

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	crtConfig "github.com/codeready-toolchain/member-operator/pkg/configuration"
	"github.com/codeready-toolchain/member-operator/pkg/rest"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	cheUserPath     = "api/user"
	cheUserFindPath = cheUserPath + "/find"
)

var log = logf.Log.WithName("che-client")

// DefaultClient is a default implementation of a CheClient
var DefaultClient *Client

// Client is a client for interacting with Che services
type Client struct {
	config     *crtConfig.Config
	httpClient *http.Client
	k8sClient  client.Client
	tokenCache *tokenCache
}

// InitDefaultCheClient initializes the default Che service instance
func InitDefaultCheClient(cfg *crtConfig.Config, cl client.Client) {
	DefaultClient = &Client{
		config:     cfg,
		httpClient: newHTTPClient(),
		k8sClient:  cl,
		tokenCache: newTokenCache(),
	}
}

// LookupAndDeleteUser looks up the Che user with the given username and deletes the user
func (c *Client) LookupAndDeleteUser(username string) error {
	// lookup user ID
	log.Info("Deleting Che user", "username", username)

	userID, err := c.getUserIDByUsername(username)
	if err != nil {
		return err
	}

	// delete the user
	return c.deleteUser(userID)

	// res, err := c.cheRequest(http.MethodGet, path.Join(cheUserPath, userID), nil)
	// if err != nil {
	// 	return err
	// }
	// defer rest.CloseResponse(res)
	// bodyString := rest.ReadBody(res.Body)
	// if res.StatusCode != http.StatusOK {
	// 	err = errors.Errorf("unable to delete Che user, Response status: %s. Response body: %s", res.Status, bodyString)
	// }

	// log.Info("Body", "value", bodyString)

	// return err
}

// getUserIDByUsername returns the user ID that maps to the given username
func (c *Client) getUserIDByUsername(username string) (string, error) {
	reqData := url.Values{}
	reqData.Set("name", username)
	res, err := c.cheRequest(http.MethodGet, cheUserFindPath, reqData)
	if err != nil {
		return "", err
	}
	defer rest.CloseResponse(res)
	cheUser, err := readCheUser(res)
	if res.StatusCode != http.StatusOK {
		err = errors.Errorf("unable to get Che user ID, Response status: %s", res.Status)
	}
	return cheUser.ID, err
}

// deleteUser deletes the Che user with the given user ID
func (c *Client) deleteUser(userID string) error {
	res, err := c.cheRequest(http.MethodDelete, path.Join(cheUserPath, userID), nil)
	if err != nil {
		return err
	}
	defer rest.CloseResponse(res)
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusNotFound {
		err = errors.Errorf("unable to delete Che user with ID %s, Response status: %s", userID, res.Status)
	}
	return err
}

func (c *Client) cheRequest(rmethod, endpoint string, queryParams url.Values) (*http.Response, error) {
	// get Che route URL
	cheURL, err := c.getCheRouteURL()
	if err != nil {
		return nil, err
	}

	// create request
	req, err := http.NewRequest(rmethod, cheURL+endpoint, nil)
	if err != nil {
		return nil, err
	}

	if queryParams != nil {
		req.URL.RawQuery = queryParams.Encode()
	}
	log.Info("Che request", "URL", req.URL.String())

	// get auth token for request
	token, err := c.tokenCache.getToken(c.k8sClient, c.config)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+token.AccessToken)

	// do the request
	return c.httpClient.Do(req)
}

func (c *Client) getCheRouteURL() (string, error) {
	cheURL, err := getRouteURL(c.k8sClient, c.config.GetCheNamespace(), c.config.GetCheRouteName())
	if err != nil {
		return "", err
	}
	log.Info("Che Keycloak Route", "URL", cheURL)
	return cheURL, nil
}

func getRouteURL(cl client.Client, namespace, name string) (string, error) {
	route := &routev1.Route{}
	namespacedName := types.NamespacedName{Namespace: namespace, Name: name}
	err := cl.Get(context.TODO(), namespacedName, route)
	if err != nil {
		return "", err
	}
	scheme := "https"
	if route.Spec.TLS == nil || *route.Spec.TLS == (routev1.TLSConfig{}) {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s/%s", scheme, route.Spec.Host, route.Spec.Path), nil
}

type CheUser struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// readCheUser extracts json with token data from the response
func readCheUser(res *http.Response) (*CheUser, error) {
	buf := new(bytes.Buffer)
	_, err := io.Copy(buf, res.Body)
	if err != nil {
		return nil, err
	}
	jsonString := strings.TrimSpace(buf.String())
	return readCheUserFromJSON(jsonString)
}

// readCheUserFromJSON parses json with a token set
func readCheUserFromJSON(jsonString string) (*CheUser, error) {
	var cheUser CheUser
	err := json.Unmarshal([]byte(jsonString), &cheUser)
	if err != nil {
		return nil, errors.Wrapf(err, "error unmarshalling Che user json %s ", jsonString)
	}
	return &cheUser, nil
}
