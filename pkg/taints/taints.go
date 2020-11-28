package taints

import (
	"context"
	"encoding/json"
	"fmt"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// Taint allows to set soft and hard limitations for scheduling and executing pods on nodes.
type Taint struct {
	client    *kubernetes.Clientset
	nodeID    string
	taintName string
	effect    v1.TaintEffect
}

// New provides a new taint.
func New(client *kubernetes.Clientset, nodeID, taintName string, effect v1.TaintEffect) *Taint {
	return &Taint{
		client:    client,
		nodeID:    nodeID,
		taintName: taintName,
		effect:    effect,
	}
}

// Enable creates the taint for a node. Creating an existing taint is a noop.
func (t *Taint) Enable() {
	preferNoSchedule(t.client, t.nodeID, t.taintName, t.effect, true)
}

// Disable removes the taint for a node. Removing a missing taint is a noop.
func (t *Taint) Disable() {
	preferNoSchedule(t.client, t.nodeID, t.taintName, t.effect, false)
}

func preferNoSchedule(client *kubernetes.Clientset, nodeID, taintName string, effect v1.TaintEffect, taintShouldExists bool) {
	updatedNode, err := client.CoreV1().Nodes().Get(context.TODO(), nodeID, metav1.GetOptions{})
	if err != nil || updatedNode == nil {
		log.Fatalf("Error reading node %s: %v", nodeID, err)
	}

	taintExists := false
	offset := 0
	for i, taint := range updatedNode.Spec.Taints {
		if taint.Key == taintName {
			taintExists = true
			offset = i
			break
		}
	}

	if taintExists && taintShouldExists {
		log.Debugf("Taint %v exists already for node %v.", taintName, nodeID)
		return
	}

	if !taintExists && !taintShouldExists {
		log.Debugf("Taint %v already missing for node %v.", taintName, nodeID)
		return
	}

	type patchTaints struct {
		Op    string      `json:"op"`
		Path  string      `json:"path"`
		Value interface{} `json:"value,omitempty"`
	}

	taint := v1.Taint{
		Key:    taintName,
		Effect: effect,
	}

	var patches []patchTaints

	if len(updatedNode.Spec.Taints) == 0 {
		// add first taint and ensure to keep current taints
		patches = []patchTaints{
			{
				Op:    "test",
				Path:  "/spec",
				Value: updatedNode.Spec,
			},
			{
				Op:    "add",
				Path:  "/spec/taints",
				Value: []v1.Taint{},
			},
			{
				Op:    "add",
				Path:  "/spec/taints/-",
				Value: taint,
			},
		}
	} else if taintExists {
		// remove taint and ensure to test against race conditions
		patches = []patchTaints{
			{
				Op:    "test",
				Path:  fmt.Sprintf("/spec/taints/%d", offset),
				Value: taint,
			},
			{
				Op:   "remove",
				Path: fmt.Sprintf("/spec/taints/%d", offset),
			},
		}
	} else {
		// add missing taint to exsting list
		patches = []patchTaints{
			{
				Op:    "add",
				Path:  "/spec/taints/-",
				Value: taint,
			},
		}
	}

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		log.Fatalf("Error encoding taint patcht for node %s: %v", nodeID, err)
	}

	_, err = client.CoreV1().Nodes().Patch(context.TODO(), nodeID, types.JSONPatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		log.Fatalf("Error patching taint for node %s: %v", nodeID, err)
	}

	if taintShouldExists {
		log.Info("Node taint added")
	} else {
		log.Info("Node taint removed")
	}
}
