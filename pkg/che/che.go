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

// UserExists returns true if the username exists, false if it doesn't and an error if there was problem with the request
func (c *Client) UserExists(username string) (bool, error) {
	log.Info("Checking if Che user exists", "username", username)
	reqData := url.Values{}
	reqData.Set("name", username)
	res, err := c.cheRequest(http.MethodGet, cheUserFindPath, reqData)
	if err != nil {
		return false, err
	}
	defer rest.CloseResponse(res)
	if res.StatusCode == http.StatusOK {
		return true, nil
	} else if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, errors.Errorf("request to find Che user '%s' failed, Response status: '%s'", username, res.Status)
}

// GetUserIDByUsername returns the user ID that maps to the given username
func (c *Client) GetUserIDByUsername(username string) (string, error) {
	log.Info("Looking up Che user ID", "username", username)
	reqData := url.Values{}
	reqData.Set("name", username)
	res, err := c.cheRequest(http.MethodGet, cheUserFindPath, reqData)
	if err != nil {
		return "", err
	}
	defer rest.CloseResponse(res)
	cheUser, err := readCheUser(res)
	if res.StatusCode != http.StatusOK {
		err = errors.Errorf("unable to get Che user ID for user '%s', Response status: '%s' Body: '%s'", username, res.Status, rest.ReadBody(res.Body))
	}
	return cheUser.ID, err
}

// DeleteUser deletes the Che user with the given user ID
func (c *Client) DeleteUser(userID string) error {
	log.Info("Deleting Che user", "userID", userID)
	res, err := c.cheRequest(http.MethodDelete, path.Join(cheUserPath, userID), nil)
	if err != nil {
		return err
	}
	defer rest.CloseResponse(res)
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusNotFound {
		err = errors.Errorf("unable to delete Che user with ID '%s', Response status: '%s' Body: '%s'", userID, res.Status, rest.ReadBody(res.Body))
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

// CheUser holds the data retrieved from the Che user API
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
