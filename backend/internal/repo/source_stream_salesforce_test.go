package repo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProgramSalesforceBusinessAliasFreezesDescriptor(t *testing.T) {
	const ref = "vendor:salesforce:object:Quote"
	source := Source{ID: 81, ConfigRevision: 1, Protocol: "oauth2", Document: `{"protocol":"oauth2","sourceType":"salesforce"}`, Resources: `[{"id":"vendor:salesforce:describe:Quote","name":"Quotes","models":["publish","request-reply"],"schema":{"minimum":9007199254740993},"metadata":{"sourceType":"salesforce","readOnly":true,"nativeIdentity":"Quote","metadataVerifiedAt":1}}]`}
	contract, err := programSourceContract(source, []string{ref})
	require.NoError(t, err)
	require.Len(t, contract.Resources, 1)
	var resources []map[string]json.RawMessage
	require.NoError(t, source.DecodeProgramResources(&resources))
	require.Len(t, resources, 2)
	require.JSONEq(t, `{"minimum":9007199254740993}`, string(resources[1]["schema"]))

	changed := source
	changed.Resources = strings.ReplaceAll(source.Resources, "9007199254740993", "9007199254740992")
	newContract, err := programSourceContract(changed, []string{ref})
	require.NoError(t, err)
	require.NotEqual(t, contract.Resources, newContract.Resources, "preserve exact JSON numbers in frozen descriptors")
	changed.Resources = strings.ReplaceAll(source.Resources, `"metadataVerifiedAt":1`, `"metadataVerifiedAt":2`)
	newContract, err = programSourceContract(changed, []string{ref})
	require.NoError(t, err)
	require.Equal(t, contract.Resources, newContract.Resources, "observation timestamp is not physical identity")
	for _, replacement := range []string{`"readOnly":false`, `"readOnly":"true"`} {
		changed.Resources = strings.ReplaceAll(source.Resources, `"readOnly":true`, replacement)
		_, err = programSourceContract(changed, []string{ref})
		require.ErrorIs(t, err, ErrInvalidArgument)
	}
	changed.Resources = strings.ReplaceAll(source.Resources, `"nativeIdentity":"Quote"`, `"nativeIdentity":"Account"`)
	_, err = programSourceContract(changed, []string{ref})
	require.ErrorIs(t, err, ErrInvalidArgument)
	changed.Resources = strings.TrimSuffix(source.Resources, "]") + `,{"id":"vendor:salesforce:object:Quote","models":["publish"]}]`
	require.ErrorIs(t, changed.DecodeProgramResources(&resources), ErrInvalidArgument, "duplicate aliases cannot shadow a discovered descriptor")
	_, err = programSourceContract(source, []string{"vendor:salesforce:object:Account"})
	require.ErrorIs(t, err, ErrInvalidArgument)
}
