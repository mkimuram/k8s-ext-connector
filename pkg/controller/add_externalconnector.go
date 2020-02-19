package controller

import (
	"github.com/mkimuram/k8s-ext-connector/pkg/controller/externalconnector"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, externalconnector.Add)
}
