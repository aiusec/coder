package cli_test

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/cli/clitest"
	"github.com/coder/coder/v2/coderd/coderdtest"
	"github.com/coder/coder/v2/codersdk"
	"github.com/coder/coder/v2/enterprise/coderd/coderdenttest"
	"github.com/coder/coder/v2/enterprise/coderd/license"
	"github.com/coder/coder/v2/testutil"
)

func TestAIGatewayKeys(t *testing.T) {
	t.Parallel()

	dv := coderdtest.DeploymentValues(t)
	dv.AI.BridgeConfig.Enabled = true
	ownerClient, owner := coderdenttest.New(t, &coderdenttest.Options{
		Options: &coderdtest.Options{
			DeploymentValues: dv,
		},
		LicenseOptions: &coderdenttest.LicenseOptions{
			Features: license.Features{
				codersdk.FeatureAIBridge: 1,
			},
		},
	})
	t.Run("CRUD", func(t *testing.T) {
		t.Parallel()
		ctx := testutil.Context(t, testutil.WaitLong)

		// List returns empty when no keys exist.
		inv, root := newCLI(t, "ai", "gateway", "keys", "list")
		clitest.SetupConfig(t, ownerClient, root)
		out := bytes.NewBuffer(nil)
		stderr := bytes.NewBuffer(nil)
		inv.Stdout = out
		inv.Stderr = stderr

		err := inv.WithContext(ctx).Run()
		require.NoError(t, err)
		require.Empty(t, out.String())
		require.Contains(t, stderr.String(), "No AI Gateway keys found.")

		// Create two keys and capture their IDs and prefixes from output.
		keyNames := []string{"gateway-key-a", "gateway-key-b"}
		createRe := regexp.MustCompile(`ID: ([0-9a-f-]+), Prefix: (\S+)\)`)
		type createdKey struct {
			id     uuid.UUID
			prefix string
		}
		created := make([]createdKey, 0, len(keyNames))
		for _, name := range keyNames {
			inv, root = newCLI(t, "ai", "gateway", "keys", "create", name)
			clitest.SetupConfig(t, ownerClient, root)
			out = bytes.NewBuffer(nil)
			inv.Stdout = out

			err = inv.WithContext(ctx).Run()
			require.NoError(t, err)
			require.Contains(t, out.String(), "Successfully created AI Gateway key "+name)

			matches := createRe.FindStringSubmatch(out.String())
			require.Len(t, matches, 3, "expected ID and Prefix in create output")
			id, err := uuid.Parse(matches[1])
			require.NoError(t, err)
			created = append(created, createdKey{id: id, prefix: matches[2]})
		}

		// List returns both created keys as JSON with matching IDs and prefixes.
		inv, root = newCLI(t, "ai", "gateway", "keys", "list", "--output=json")
		clitest.SetupConfig(t, ownerClient, root)
		out = bytes.NewBuffer(nil)
		inv.Stdout = out

		err = inv.WithContext(ctx).Run()
		require.NoError(t, err)

		var listed []codersdk.AIGatewayKey
		require.NoError(t, json.Unmarshal(out.Bytes(), &listed))
		require.Len(t, listed, 2)
		for i, key := range listed {
			require.Equal(t, keyNames[i], key.Name)
			require.Equal(t, created[i].id, key.ID)
			require.Equal(t, created[i].prefix, key.KeyPrefix)
		}

		// Delete rejects names and requires an ID.
		inv, root = newCLI(t, "ai", "gateway", "keys", "delete", "--yes", keyNames[0])
		clitest.SetupConfig(t, ownerClient, root)

		err = inv.WithContext(ctx).Run()
		require.ErrorContains(t, err, "parse AI Gateway key ID")

		// Delete the first key by ID.
		inv, root = newCLI(t, "ai", "gateway", "keys", "delete", "--yes", created[0].id.String())
		clitest.SetupConfig(t, ownerClient, root)
		out = bytes.NewBuffer(nil)
		inv.Stdout = out

		err = inv.WithContext(ctx).Run()
		require.NoError(t, err)
		require.Contains(t, out.String(), "Deleted AI Gateway key "+created[0].id.String())

		// List returns only the remaining key.
		inv, root = newCLI(t, "ai", "gateway", "keys", "list", "--output=json")
		clitest.SetupConfig(t, ownerClient, root)
		out = bytes.NewBuffer(nil)
		inv.Stdout = out

		err = inv.WithContext(ctx).Run()
		require.NoError(t, err)

		require.NoError(t, json.Unmarshal(out.Bytes(), &listed))
		require.Len(t, listed, 1)
		require.Equal(t, keyNames[1], listed[0].Name)

		// Delete the second key.
		inv, root = newCLI(t, "ai", "gateway", "keys", "delete", "--yes", created[1].id.String())
		clitest.SetupConfig(t, ownerClient, root)

		err = inv.WithContext(ctx).Run()
		require.NoError(t, err)

		// List returns empty after all keys deleted.
		inv, root = newCLI(t, "ai", "gateway", "keys", "list", "--output=json")
		clitest.SetupConfig(t, ownerClient, root)
		out = bytes.NewBuffer(nil)
		inv.Stdout = out

		err = inv.WithContext(ctx).Run()
		require.NoError(t, err)

		require.NoError(t, json.Unmarshal(out.Bytes(), &listed))
		require.Empty(t, listed)

		// Delete a non-existent key returns not found.
		missingID := uuid.New()
		inv, root = newCLI(t, "ai", "gateway", "keys", "delete", "--yes", missingID.String())
		clitest.SetupConfig(t, ownerClient, root)

		err = inv.WithContext(ctx).Run()
		require.ErrorContains(t, err, missingID.String())
		require.ErrorContains(t, err, "not found")
	})

	t.Run("InvalidKeyName", func(t *testing.T) {
		t.Parallel()
		ctx := testutil.Context(t, testutil.WaitLong)

		inv, root := newCLI(t, "ai", "gateway", "keys", "create", strings.Repeat("a", 65))
		clitest.SetupConfig(t, ownerClient, root)

		err := inv.WithContext(ctx).Run()
		require.ErrorContains(t, err, "create AI Gateway key")
		require.ErrorContains(t, err, "Invalid key name")
	})

	t.Run("MemberForbidden", func(t *testing.T) {
		t.Parallel()
		ctx := testutil.Context(t, testutil.WaitLong)

		memberClient, _ := coderdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)

		for _, args := range [][]string{
			{"ai", "gateway", "keys", "list"},
			{"ai", "gateway", "keys", "create", "member-key"},
			{"ai", "gateway", "keys", "delete", "--yes", uuid.NewString()},
		} {
			inv, root := newCLI(t, args...)
			clitest.SetupConfig(t, memberClient, root)

			err := inv.WithContext(ctx).Run()
			require.Error(t, err)
			require.ErrorContains(t, err, "Forbidden")
		}
	})
}
