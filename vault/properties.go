package vault

import (
	"context"
	"path"
	"slices"

	"github.com/swiftj/obsidian-mcp/vault/props"
)

// PropertyTypes reads the vault's recorded property types from
// <config folder>/types.json (P-TYPE-01). A missing file gives none.
func (v *Vault) PropertyTypes(ctx context.Context) (props.Types, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, _, err := v.readRegular("config", v.pol.configDir+"/types.json")
	if isNotFound(err) {
		return props.Types{}, nil
	}
	if err != nil {
		return nil, err
	}
	return props.ParseTypes(data)
}

// SetProperties sets each property in set on the note at p, in name order.
// Values are checked against the vault's recorded types, and only the
// changed properties' lines are rewritten.
func (v *Vault) SetProperties(ctx context.Context, p, etag string, set map[string]any, o WriteOptions) (*Result, error) {
	types, err := v.PropertyTypes(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(set))
	for k := range set {
		names = append(names, k)
	}
	slices.Sort(names)
	return v.propertyWrite(ctx, p, etag, o, func(src []byte) ([]byte, error) {
		for _, n := range names {
			var err error
			if src, err = props.Set(src, n, set[n], props.SetOptions{Types: types}); err != nil {
				return nil, err
			}
		}
		return src, nil
	})
}

// RemoveProperties removes the named properties from the note at p.
func (v *Vault) RemoveProperties(ctx context.Context, p, etag string, names []string, o WriteOptions) (*Result, error) {
	return v.propertyWrite(ctx, p, etag, o, func(src []byte) ([]byte, error) {
		for _, n := range names {
			var err error
			if src, err = props.Remove(src, n); err != nil {
				return nil, err
			}
		}
		return src, nil
	})
}

// RenameProperty renames a property of the note at p, keeping its value.
func (v *Vault) RenameProperty(ctx context.Context, p, etag, from, to string, o WriteOptions) (*Result, error) {
	return v.propertyWrite(ctx, p, etag, o, func(src []byte) ([]byte, error) {
		return props.Rename(src, from, to)
	})
}

func (v *Vault) propertyWrite(ctx context.Context, p, etag string, o WriteOptions, change func([]byte) ([]byte, error)) (*Result, error) {
	if path.Ext(p) != ".md" {
		return nil, &PathError{Op: "properties", Path: p, Reason: "only notes (.md) have properties"}
	}
	if etag == "" {
		return nil, ErrETagRequired
	}
	return v.replace(ctx, p, etag, o, change)
}
