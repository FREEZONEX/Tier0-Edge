package streamexec

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProgramCryptoDeterministicSigning(t *testing.T) {
	code := `const crypto=require("node:crypto");
 const registeredHash=crypto.createHash("sha256").update("abc").digest("hex");
 module.exports=api=>{api.stream.onRequest({id:"r"},()=>({
  registeredHash,
  hash:crypto.createHash("sha256").update("a").update("bc").digest("hex"),
  hmac:crypto.createHmac("sha256","test-key").update("签名内容").digest("base64"),
  empty:crypto.createHash("sha256").update("").digest("base64"),
  keys:Object.keys(crypto).sort(),same:require("node:crypto")===crypto
 }));};`
	req := programRequest(code, "r")
	req.CheckOnly = true
	_, err := ExecuteProgram(context.Background(), req, nil)
	require.NoError(t, err, "crypto is deterministic and has no registration I/O")
	req.CheckOnly = false
	result, err := ExecuteProgram(context.Background(), req, nil)
	require.NoError(t, err)
	abc := sha256.Sum256([]byte("abc"))
	empty := sha256.Sum256(nil)
	mac := hmac.New(sha256.New, []byte("test-key"))
	_, _ = mac.Write([]byte("签名内容"))
	expected, err := json.Marshal(map[string]interface{}{
		"registeredHash": hex.EncodeToString(abc[:]), "hash": hex.EncodeToString(abc[:]),
		"hmac": base64.StdEncoding.EncodeToString(mac.Sum(nil)), "empty": base64.StdEncoding.EncodeToString(empty[:]),
		"keys": []string{"createHash", "createHmac"}, "same": true,
	})
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(result.Data))
}

func TestProgramCryptoWhitelistAndArgumentLimits(t *testing.T) {
	for _, body := range []string{
		`require("crypto");`, `require("fs");`, `require("node:http");`, `require("child_process");`, `require("lodash");`, `require({toString:()=>"node:crypto"});`,
		`crypto.randomBytes(16);`, `crypto.createHash("md5");`, `crypto.createHash("SHA256");`, `crypto.createHash("sha256",{});`,
		`crypto.createHmac("sha256",42);`, `crypto.createHmac("sha256");`, `crypto.createHmac("sha256","x".repeat(1048577));`,
		`crypto.createHash("sha256").update({toString:()=>"a"});`, `crypto.createHash("sha256").update("x","hex");`,
		`crypto.createHash("sha256").update("x".repeat(1048576)).update("x");`,
		`crypto.createHash("sha256").digest();`, `crypto.createHash("sha256").digest("utf8");`,
		`const h=crypto.createHash("sha256");h.digest("hex");h.update("a");`,
		`const h=crypto.createHash("sha256");h.digest("hex");h.digest("base64");`,
		`crypto.createHash=()=>"forged";`,
	} {
		t.Run(body, func(t *testing.T) {
			req := programRequest(`const crypto=require("node:crypto");module.exports=api=>{api.stream.onRequest({id:"r"},()=>{`+body+`return 1;});};`, "r")
			_, err := ExecuteProgram(context.Background(), req, nil)
			require.ErrorIs(t, err, ErrExecution)
		})
	}
}

func TestProgramCryptoSecretUsedOnlyThroughControlledHost(t *testing.T) {
	req := programRequest(`const crypto=require("node:crypto");module.exports=api=>{
 const source=api.sources.use({id:"1",resources:[]});
 api.stream.onRequest({id:"r"},async()=>{const secrets=await source.secrets();return crypto.createHmac("sha256",secrets.signingKey).update("body").digest("hex");});
};`, "r")
	calls := 0
	result, err := ExecuteProgram(context.Background(), req, func(_ context.Context, call ProgramCall) (json.RawMessage, error) {
		require.Equal(t, "source.secrets", call.Kind)
		require.Equal(t, "1", call.SourceID)
		require.Empty(t, call.Input)
		require.Empty(t, call.Options)
		calls++
		return json.RawMessage(`{"signingKey":"test-only-key"}`), nil
	})
	require.NoError(t, err)
	mac := hmac.New(sha256.New, []byte("test-only-key"))
	_, _ = mac.Write([]byte("body"))
	expected, err := json.Marshal(hex.EncodeToString(mac.Sum(nil)))
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(result.Data))
	require.Empty(t, result.Outputs)
	require.Empty(t, result.Logs)
	require.Equal(t, 1, calls)
}
