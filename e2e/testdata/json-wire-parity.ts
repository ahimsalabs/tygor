import { StringEncodingDepthsSchema, DefinedByteSlicesSchema, CustomElementByteSliceSchema, AliasResultMarshalersSchema, UnconstrainedCustomMarshalerPayloadSchema } from './schemas.zod';
import { StringEncodingDepthsSchema as MiniStringEncodingDepthsSchema, DefinedByteSlicesSchema as MiniDefinedByteSlicesSchema, CustomElementByteSliceSchema as MiniCustomElementByteSliceSchema, AliasResultMarshalersSchema as MiniAliasResultMarshalersSchema, UnconstrainedCustomMarshalerPayloadSchema as MiniUnconstrainedCustomMarshalerPayloadSchema } from './schemas.zod-mini';
import type { StringEncodingDepths, DefinedByteSlices, CustomElementByteSlice, AliasResultMarshalers, UnconstrainedCustomMarshalerPayload } from './types';

const typedDepths: StringEncodingDepths = { direct: "7", single: "7", double: 7, triple: 7, duration: "1000000000", defined: __DEFINED_LITERAL__ };
const typedBytes: DefinedByteSlices = { data: "AQI=" };
const typedCustomBytes: CustomElementByteSlice = { data: ["octet"] };
const typedAliasMarshalers: AliasResultMarshalers = { json_value: "json-value", json_pointer: "json-pointer", text_value: "text-value", text_pointer: "text-pointer" };
const typedUnconstrained: UnconstrainedCustomMarshalerPayload = { box: { value: "json-value" } };
const depths = JSON.parse(__DEPTH_JSON__);
const bytes = JSON.parse(__BYTE_JSON__);
const customBytes = JSON.parse(__CUSTOM_BYTE_JSON__);
const aliasMarshalers = JSON.parse(__ALIAS_MARSHALER_JSON__);
const unconstrained = JSON.parse(__UNCONSTRAINED_JSON__);
void [typedDepths, typedBytes, typedCustomBytes, typedAliasMarshalers, typedUnconstrained];
for (const schema of [StringEncodingDepthsSchema, MiniStringEncodingDepthsSchema]) {
  schema.parse(depths);
  if (schema.safeParse({ ...depths, single: 7 }).success) throw new Error('accepted unquoted *int');
  if (schema.safeParse({ ...depths, double: "7" }).success) throw new Error('accepted quoted **int');
  if (schema.safeParse({ ...depths, duration: 1000000000 }).success) throw new Error('accepted unquoted duration');
  if (schema.safeParse({ ...depths, duration: "9223372036854775808" }).success) throw new Error('accepted overflowing duration');
  const wrongDefined = typeof depths.defined === "string" ? 7 : "7";
  if (schema.safeParse({ ...depths, defined: wrongDefined }).success) throw new Error('accepted wrong defined-pointer wire type');
}
for (const schema of [DefinedByteSlicesSchema, MiniDefinedByteSlicesSchema]) {
  schema.parse(bytes);
  if (schema.safeParse({ data: [1, 2] }).success) throw new Error('accepted []Octet as an array');
}
for (const schema of [CustomElementByteSliceSchema, MiniCustomElementByteSliceSchema]) {
  schema.parse(customBytes);
  if (schema.safeParse({ data: "AQI=" }).success) throw new Error('accepted marshaled octets as base64');
}
for (const schema of [AliasResultMarshalersSchema, MiniAliasResultMarshalersSchema]) {
  schema.parse(aliasMarshalers);
}
for (const schema of [UnconstrainedCustomMarshalerPayloadSchema, MiniUnconstrainedCustomMarshalerPayloadSchema]) {
  schema.parse(unconstrained);
}
