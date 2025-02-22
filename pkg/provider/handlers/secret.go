package handlers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/containerd/containerd"
	"github.com/openfaas/faas-provider/types"
)

const secretFilePermission = 0644
const secretDirPermission = 0755

func MakeSecretHandler(c *containerd.Client, mountPath string) func(w http.ResponseWriter, r *http.Request) {

	err := os.MkdirAll(mountPath, secretFilePermission)
	if err != nil {
		log.Printf("Creating path: %s, error: %s\n", mountPath, err)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			defer r.Body.Close()
		}

		switch r.Method {
		case http.MethodGet:
			listSecrets(c, w, r, mountPath)
		case http.MethodPost:
			createSecret(c, w, r, mountPath)
		case http.MethodPut:
			createSecret(c, w, r, mountPath)
		case http.MethodDelete:
			deleteSecret(c, w, r, mountPath)
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}

	}
}

func listSecrets(c *containerd.Client, w http.ResponseWriter, r *http.Request, mountPath string) {

	lookupNamespace := getRequestNamespace(readNamespaceFromQuery(r))
	// Check if namespace exists, and it has the openfaas label
	valid, err := validNamespace(c, lookupNamespace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if !valid {
		http.Error(w, "namespace not valid", http.StatusBadRequest)
		return
	}

	mountPath = getNamespaceSecretMountPath(mountPath, lookupNamespace)

	files, err := ioutil.ReadDir(mountPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	secrets := []types.Secret{}
	for _, f := range files {
		secrets = append(secrets, types.Secret{Name: f.Name(), Namespace: lookupNamespace})
	}

	bytesOut, _ := json.Marshal(secrets)
	w.Write(bytesOut)
}

func createSecret(c *containerd.Client, w http.ResponseWriter, r *http.Request, mountPath string) {
	secret, err := parseSecret(r)
	if err != nil {
		log.Printf("[secret] error %s", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = validateSecret(secret)
	if err != nil {
		log.Printf("[secret] error %s", err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("[secret] is valid: %q", secret.Name)
	namespace := getRequestNamespace(secret.Namespace)
	mountPath = getNamespaceSecretMountPath(mountPath, namespace)

	err = os.MkdirAll(mountPath, secretDirPermission)
	if err != nil {
		log.Printf("[secret] error %s", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := secret.RawValue
	if len(data) == 0 {
		data = []byte(secret.Value)
	}

	err = ioutil.WriteFile(path.Join(mountPath, secret.Name), data, secretFilePermission)

	if err != nil {
		log.Printf("[secret] error %s", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func deleteSecret(c *containerd.Client, w http.ResponseWriter, r *http.Request, mountPath string) {
	secret, err := parseSecret(r)
	if err != nil {
		log.Printf("[secret] error %s", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	namespace := getRequestNamespace(readNamespaceFromQuery(r))
	mountPath = getNamespaceSecretMountPath(mountPath, namespace)

	err = os.Remove(path.Join(mountPath, secret.Name))

	if err != nil {
		log.Printf("[secret] error %s", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func parseSecret(r *http.Request) (types.Secret, error) {
	secret := types.Secret{}
	bytesOut, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return secret, err
	}

	err = json.Unmarshal(bytesOut, &secret)
	if err != nil {
		return secret, err
	}

	return secret, err
}

const traverseErrorSt = "directory traversal found in name"

func isTraversal(name string) bool {
	return strings.Contains(name, fmt.Sprintf("%s", string(os.PathSeparator))) ||
		strings.Contains(name, "..")
}

func validateSecret(secret types.Secret) error {
	if strings.TrimSpace(secret.Name) == "" {
		return fmt.Errorf("non-empty name is required")
	}
	if isTraversal(secret.Name) {
		return fmt.Errorf(traverseErrorSt)
	}
	return nil
}
