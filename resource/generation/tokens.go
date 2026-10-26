package generation

import "strings"

// The descriptor's tokens block (typescriptAPITemplate): what the console's token page
// and device page configure themselves from. The session outlet whose Auth issues tokens
// for the application's machine outlets carries it, naming the token routes the
// generated router mounts on that outlet (linkTokenIssuers) and the machine outlets a
// token is issued for.

// The segments under the token routes: a token revoked by id, a device request approved
// and denied.
const (
	tokensRevokeSegment  = "/revoke"
	deviceApproveSegment = "/approve"
	deviceDenySegment    = "/deny"
)

// tokenIssuers derives the tokens block of every session outlet whose Auth issues
// tokens for a machine outlet, by outlet name. The issuer of a machine outlet is the
// first session outlet declaring the auth its Tokens names, the rule the generated
// router mounts the token routes by, and its block lists every machine outlet the auth
// issues for, in declaration order; a second session outlet declaring the same auth
// and an outlet that only manages it get none. Nothing where the run generates no
// routes.
func (r *resourceGenerator) tokenIssuers() map[string]*tsAPITokens {
	if !r.genRoutes {
		return nil
	}
	outlets := r.allOutlets()
	issuers := make(map[string]*tsAPITokens)
	for _, machine := range outlets {
		if !machine.apiKey || machine.tokenAuth == "" {
			continue
		}
		issuer := tokenIssuerOf(outlets, machine.tokenAuth)
		if issuer == nil {
			continue
		}
		block, ok := issuers[issuer.name]
		if !ok {
			block = tokensBlock()
			issuers[issuer.name] = block
		}
		block.Outlets = append(block.Outlets, &tsAPIMachineOutlet{Name: machine.name, Prefix: "/" + machine.prefix})
	}

	return issuers
}

// tokenIssuerOf is the first session outlet declaring the auth at importPath, nil when
// none does (which validateRouterConfig refuses before the descriptor is rendered).
func tokenIssuerOf(outlets []*routerOutlet, importPath string) *routerOutlet {
	for _, outlet := range outlets {
		if outlet.auth != nil && outlet.auth.importPath == importPath {
			return outlet
		}
	}

	return nil
}

// tokensBlock is the block's routes, the token and device routes the generated router
// mounts on the issuing outlet without their leading slash, with no outlets yet.
func tokensBlock() *tsAPITokens {
	tokens := strings.TrimPrefix(userTokensSuffix, "/")
	device := strings.TrimPrefix(userDeviceSuffix, "/")

	return &tsAPITokens{
		Route:        tokens,
		RevokeRoute:  tokens + tokensRevokeSegment,
		DeviceRoute:  device,
		ApproveRoute: device + deviceApproveSegment,
		DenyRoute:    device + deviceDenySegment,
	}
}

// tokensOn is the target outlet's tokens block, nil where its Auth issues no tokens.
func (t *typescriptGenerator) tokensOn() *tsAPITokens {
	return t.tokenIssuers[t.targetOutlet()]
}
