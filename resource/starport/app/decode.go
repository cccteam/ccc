package app

import (
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/starport/pkg/rpc"
)

// NewQueryDecoder builds a query decoder for a resource and request pair.
func NewQueryDecoder[Resource Resourcer, Request any](_ *App, permissions ...accesstypes.Permission) *resource.QueryDecoder[Resource, Request] {
	rSet, err := resource.NewSet[Resource, Request](permissions...)
	if err != nil {
		panic(err)
	}

	decoder, err := resource.NewQueryDecoder[Resource, Request](rSet)
	if err != nil {
		panic(err)
	}

	return decoder
}

// NewDecoder builds a patch decoder for a resource and request pair.
func NewDecoder[Resource Resourcer, Request any](a *App, permissions ...accesstypes.Permission) *resource.Decoder[Resource, Request] {
	rSet, err := resource.NewSet[Resource, Request](permissions...)
	if err != nil {
		panic(err)
	}

	decoder, err := resource.NewDecoder[Resource, Request](rSet)
	if err != nil {
		panic(err)
	}

	return decoder.WithValidator(a.validate)
}

// NewRPCDecoder builds a decoder for an RPC method request.
func NewRPCDecoder[Method rpc.Method, Request any](a *App, perm accesstypes.Permission) *resource.RPCDecoder[Request] {
	var method Method
	res := method.Method()

	decoder, err := resource.NewRPCDecoder[Request](a.UserPermissions, res, perm)
	if err != nil {
		panic(err)
	}

	return decoder.WithValidator(a.validate)
}
