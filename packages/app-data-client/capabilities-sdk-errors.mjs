// The real SDK transport consumes real Enterprise handler/middleware errors.
import assert from 'node:assert/strict';
import {pathToFileURL} from 'node:url';
const {CapabilitiesClient,CapabilityError}=await import(pathToFileURL(process.env.CAPABILITY_SDK_MODULE));
const status=Number(process.env.CAPABILITY_EXPECTED_STATUS),errorCode=process.env.CAPABILITY_EXPECTED_CODE;
const originalFetch=globalThis.fetch;
let calls=0;
globalThis.fetch=async(url,init)=>{
 calls++;
 if(status===413)init={...init,body:" ".repeat(1048577)+"{}"};
 if(status===400)init={...init,body:'{"scope":"all"}'};
 const response=await originalFetch(url,init);
 assert.equal(response.status,status);
 const body=await response.clone().json();
 assert.deepEqual(Object.keys(body).sort(),['errorCode','message']);
 return response;
};
const client=new CapabilitiesClient(()=>({getBaseURL:()=>process.env.CAPABILITY_TEST_BASE,getApiKey:()=>process.env.CAPABILITY_TEST_KEY||''}));
await assert.rejects(client.discover(),error=>error instanceof CapabilityError&&error.status===status&&error.errorCode===errorCode&&error.retryable===false);
assert.equal(calls,1);
