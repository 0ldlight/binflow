package adapter

import "context"

// DeployProps is the deploy-time property set peeled off a content path's
// matrix parameters ("PUT repo/a/b.bin;build=77;env=prod" -> the node
// a/b.bin carries build=[77], env=[prod]). The map's shape is
// repo.PutOptions.Properties' — assignment between the two is implicit,
// so the adapter seam never copies.
type DeployProps map[string][]string

// deployPropsKey is the context key carrying the deploy properties of the
// request at hand (nil/empty = none). httpapi's dispatch hands the adapter
// the request; the adapter's ServeHTTP resolves the path ONCE
// (ResolveContent), boxes the properties here, and its put handlers read
// them back when assembling repo.PutOptions — the WithPrincipal twin
// (architecture section 15.3.1: "props 经 request context 注入 adapter").
type deployPropsKey struct{}

// WithDeployProps returns a context carrying props for the adapter's put
// handlers. props may be nil (a path without matrix parameters): the value
// is stored as a typed box so DeployPropsFrom can distinguish "none" from
// "absent" — both read as nil, but keeping the box symmetric with
// WithPrincipal reads the same at every call site.
func WithDeployProps(ctx context.Context, props DeployProps) context.Context {
	return context.WithValue(ctx, deployPropsKey{}, deployPropsBox{props: props})
}

// DeployPropsFrom resolves the request's deploy properties: the matrix set
// ServeHTTP boxed after ResolveContent, or nil when the path carried none.
// Reads never consult it (properties do not participate in lookup);
// GET/HEAD/DELETE address the stripped path only.
func DeployPropsFrom(ctx context.Context) DeployProps {
	box, ok := ctx.Value(deployPropsKey{}).(deployPropsBox)
	if !ok {
		return nil
	}
	return box.props
}

type deployPropsBox struct{ props DeployProps }
