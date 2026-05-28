// Package ekmadapter keeps node-specific bridges from root-module types to the
// standalone ssvsigner contracts.
//
// It lives in the root module because the adapter depends on node-only storage
// types from storage/basedb, while ssvsigner must remain importable without any
// dependency on the root module. The "adapter" naming is intentional: this
// package translates the node's database interface into ekm.Database without
// leaking basedb into ssvsigner.
package ekmadapter
