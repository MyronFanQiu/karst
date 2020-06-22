package tee

import (
	"encoding/json"
	"errors"
	"fmt"
	"karst/logger"
	"karst/merkletree"

	"github.com/gorilla/websocket"
)

type sealedMessage struct {
	Status int
	Body   string
	Path   string
}

type unsealBackMessage struct {
	Status int
	Body   string
	Path   string
}

type Tee struct {
	BaseUrl string
	Backup  string
}

func NewTee(baseUrl string, backup string) (*Tee, error) {
	if backup == "" || baseUrl == "" {
		return nil, errors.New("Fatal error in getting backup and tee base url")
	}

	return &Tee{
		BaseUrl: baseUrl,
		Backup:  backup,
	}, nil
}

// TODO: change to wss
func (tee *Tee) Seal(path string, merkleTree *merkletree.MerkleTreeNode) (*merkletree.MerkleTreeNode, string, error) {
	// Connect to tee
	url := tee.BaseUrl + "/storage/seal"
	logger.Info("Connecting to TEE '%s' to seal file", url)
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, "", err
	}
	defer c.Close()

	// Send file to seal
	reqBody := map[string]interface{}{
		"backup": tee.Backup,
		"body":   merkleTree,
		"path":   path,
	}

	reqBodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", err
	} else {
		logger.Debug("Request body for sealing: %s", string(reqBodyBytes))
	}

	err = c.WriteMessage(websocket.TextMessage, reqBodyBytes)
	if err != nil {
		return nil, "", err
	}

	// Deal result
	_, message, err := c.ReadMessage()
	if err != nil {
		return nil, "", err
	}
	logger.Debug("Recv: %s", message)

	var sealedMsg sealedMessage
	err = json.Unmarshal([]byte(message), &sealedMsg)
	if err != nil {
		return nil, "", fmt.Errorf("Unmarshal seal result failed: %s", err)
	}

	if sealedMsg.Status != 200 {
		return nil, "", fmt.Errorf("Seal failed, error code is %d", sealedMsg.Status)
	}

	var merkleTreeSealed merkletree.MerkleTreeNode
	if err = json.Unmarshal([]byte(sealedMsg.Body), &merkleTreeSealed); err != nil {
		return nil, "", fmt.Errorf("Unmarshal sealed merkle tree failed: %s", err)
	}

	return &merkleTreeSealed, sealedMsg.Path, nil
}

func (tee *Tee) Unseal(path string) (*merkletree.MerkleTreeNode, string, error) {
	// Connect to tee
	url := tee.BaseUrl + "/storage/unseal"
	logger.Info("Connecting to TEE '%s' to unseal file", url)
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, "", err
	}
	defer c.Close()

	// Send file to seal
	reqBody := map[string]interface{}{
		"backup": tee.Backup,
		"path":   path,
	}

	reqBodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", err
	} else {
		logger.Debug("Request body for unsealing: %s", string(reqBodyBytes))
	}

	err = c.WriteMessage(websocket.TextMessage, reqBodyBytes)
	if err != nil {
		return nil, "", err
	}

	// Deal result
	_, message, err := c.ReadMessage()
	if err != nil {
		return nil, "", err
	}
	logger.Debug("Recv: %s", message)

	var unsealBackMes unsealBackMessage
	err = json.Unmarshal([]byte(message), &unsealBackMes)
	if err != nil {
		return nil, "", fmt.Errorf("Unmarshal unseal back message failed: %s", err)
	}
	if unsealBackMes.Status != 200 {
		return nil, "", fmt.Errorf("Unseal failed: %s", unsealBackMes.Body)
	}

	return nil, unsealBackMes.Path, nil
}
