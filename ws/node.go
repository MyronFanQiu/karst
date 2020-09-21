package ws

import (
	"encoding/json"
	"fmt"
	"karst/logger"
	"karst/model"
	"net/http"

	"github.com/gorilla/websocket"
)

// URL: /node/data
func nodeData(w http.ResponseWriter, r *http.Request) {
	// Upgrade http to ws
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("(NodeData) Upgrade: %s", err)
		return
	}
	defer c.Close()

	// Check backup
	mt, message, err := c.ReadMessage()
	if err != nil {
		logger.Error("(NodeData) Read err: %s", err)
		return
	}

	if mt != websocket.TextMessage {
		logger.Error("(NodeData) Wrong message type is %d", mt)
		err = c.WriteMessage(websocket.TextMessage, []byte("{ \"status\": 400 }"))
		if err != nil {
			logger.Error("(NodeData) Write err: %s", err)
		}
		return
	}

	var backupMes model.BackupMessage
	err = json.Unmarshal([]byte(message), &backupMes)
	if err != nil {
		logger.Error("(NodeData) Unmarshal failed: %s", err)
		err = c.WriteMessage(websocket.TextMessage, []byte("{ \"status\": 400 }"))
		if err != nil {
			logger.Error("(NodeData) Write err: %s", err)
		}
		return
	}

	if backupMes.Backup != cfg.Crust.Backup {
		logger.Error("(NodeData) Need right backup")
		err = c.WriteMessage(websocket.TextMessage, []byte("{ \"status\": 400 }"))
		if err != nil {
			logger.Error("(NodeData) Write err: %s", err)
		}
		return
	}

	// Send right backup message
	err = c.WriteMessage(websocket.TextMessage, []byte("{ \"status\": 200 }"))
	if err != nil {
		logger.Error("(NodeData) Write err: %s", err)
	}

	// Get and send node data
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			return
		}

		if mt != websocket.TextMessage {
			return
		}

		// logger.Debug("(NodeData) Recv node data get message: %s, message type is %d", message, mt)

		var nodeDataMsg model.NodeDataMessage
		err = json.Unmarshal([]byte(message), &nodeDataMsg)
		if err != nil {
			logger.Error("(NodeData) Unmarshal failed: %s", err)
			err = c.WriteMessage(websocket.TextMessage, []byte("{ \"status\": 400 }"))
			if err != nil {
				logger.Error("(NodeData) Write err: %s", err)
			}
			return
		}

		// Get node of file
		if fs == nil {
			err = c.WriteMessage(websocket.TextMessage, []byte("{ \"status\": 404 }"))
			if err != nil {
				logger.Error("(NodeData) Write err: %s", err)
			}
			continue
		}

		fileInfo, err := model.GetFileInfoFromDb(nodeDataMsg.FileHash, db, model.SealedFileFlagInDb)
		if err != nil {
			logger.Error("(NodeData) Read file info of '%s' failed: %s", nodeDataMsg.FileHash, err)
			err = c.WriteMessage(websocket.TextMessage, []byte("{ \"status\": 404 }"))
			if err != nil {
				logger.Error("(NodeData) Write err: %s", err)
			}
			continue
		}

		if nodeDataMsg.NodeIndex > fileInfo.MerkleTreeSealed.LinksNum-1 {
			logger.Error("(NodeData) Bad request, node index is out of range")
			err = c.WriteMessage(websocket.TextMessage, []byte("{ \"status\": 400 }"))
			if err != nil {
				logger.Error("(NodeData) Write err: %s", err)
			}
			continue
		}

		nodeInfo := fileInfo.MerkleTreeSealed.Links[nodeDataMsg.NodeIndex]
		// nodeInfoBytes, _ := json.Marshal(nodeInfo)
		// logger.Debug("(NodeData) Node info in db: %s", string(nodeInfoBytes))

		if nodeInfo.Hash != nodeDataMsg.NodeHash {
			logger.Error("(NodeData) Bad request, request node hash is '%s', db node hash is '%s'", nodeDataMsg.NodeHash, nodeInfo.Hash)
			err = c.WriteMessage(websocket.TextMessage, []byte("{ \"status\": 400 }"))
			if err != nil {
				logger.Error("(NodeData) Write err: %s", err)
			}
			continue
		}

		fileBytes, err := fs.GetToBuffer(nodeInfo.StoredKey, nodeInfo.Size)
		if err != nil {
			logger.Error("(NodeData) Read file '%s' failed: %s", nodeInfo.Hash, err)
			err = c.WriteMessage(websocket.TextMessage, []byte("{ \"status\": 404 }"))
			if err != nil {
				logger.Error("(NodeData) Write err: %s", err)
			}
			continue
		}

		err = c.WriteMessage(websocket.BinaryMessage, fileBytes)
		if err != nil {
			logger.Error("(NodeData) Write err: %s", err)
			return
		}
	}
}

// URL: /node/info
func nodeInfo(w http.ResponseWriter, r *http.Request) {
	// Upgrade http to ws
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("Upgrade: %s", err)
		return
	}
	defer c.Close()

	nodeInfoReturnMsg := model.NodeInfoReturnMessage{
		Status: 200,
	}

	// request
	mt, message, err := c.ReadMessage()
	if err != nil {
		logger.Error("Read err: %s", err)
		nodeInfoReturnMsg.Info = err.Error()
		nodeInfoReturnMsg.Status = 500
		model.SendTextMessage(c, nodeInfoReturnMsg)
		return
	}

	if mt != websocket.TextMessage {
		nodeInfoReturnMsg.Info = fmt.Sprintf("Wrong message type is %d", mt)
		logger.Error(nodeInfoReturnMsg.Info)
		nodeInfoReturnMsg.Status = 400
		model.SendTextMessage(c, nodeInfoReturnMsg)
		return
	}

	if string(message) == "address" {
		nodeInfoReturnMsg.Fastdfs = cfg.Fs.Fastdfs.OuterTrackerAddrs
		nodeInfoReturnMsg.Ipfs = cfg.Fs.Ipfs.OuterBaseUrl
		model.SendTextMessage(c, nodeInfoReturnMsg)
	} else {
		nodeInfoReturnMsg.Info = fmt.Sprintf("Not support this request: %s", string(message))
		nodeInfoReturnMsg.Status = 400
		model.SendTextMessage(c, nodeInfoReturnMsg)
	}
}
