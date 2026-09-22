import type { Manifest } from './manifest';
import { schemaMap } from './schemas.map.zod';
import { schemaMap as miniSchemaMap } from './schemas.map.zod-mini';

const payload = JSON.parse(__BYTE_JSON__);
const custom = JSON.parse(__CUSTOM_JSON__);
const nested = JSON.parse(__NESTED_JSON__);
const phantom = JSON.parse(__PHANTOM_JSON__);
const request: Manifest["Bytes.Echo"]["req"] = { data: [1, 2] };
const response: Manifest["Bytes.Echo"]["res"] = { data: [1, 2] };
const customRequest: Manifest["Bytes.Custom"]["req"] = { data: ["octet"] };
const nestedRequest: Manifest["Bytes.Nested"]["req"] = { data: { items: [[1, 2]] } };
const phantomRequest: Manifest["Bytes.Phantom"]["req"] = { ok: true };
void [request, response, customRequest, nestedRequest, phantomRequest];
for (const schemas of [schemaMap["Bytes.Echo"], miniSchemaMap["Bytes.Echo"]]) {
  schemas.request.parse(payload);
  schemas.response.parse(payload);
  if (schemas.request.safeParse({ data: "AQI=" }).success) throw new Error('accepted defined bytes as base64');
  if (schemas.response.safeParse({ data: "AQI=" }).success) throw new Error('accepted defined bytes as base64');
}
for (const schemas of [schemaMap["Bytes.Custom"], miniSchemaMap["Bytes.Custom"]]) {
  schemas.request.parse(custom);
  schemas.response.parse(custom);
  if (schemas.request.safeParse({ data: "AQI=" }).success) throw new Error('accepted custom-marshaled values as base64');
}
for (const schemas of [schemaMap["Bytes.Nested"], miniSchemaMap["Bytes.Nested"]]) {
  schemas.request.parse(nested);
  schemas.response.parse(nested);
  if (schemas.request.safeParse({ data: { items: ["AQI="] } }).success) throw new Error('accepted nested defined bytes as base64');
}
for (const schemas of [schemaMap["Bytes.Phantom"], miniSchemaMap["Bytes.Phantom"]]) {
  schemas.request.parse(phantom);
  schemas.response.parse(phantom);
}
