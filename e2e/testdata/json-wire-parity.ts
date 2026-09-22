import { StringEncodingDepthsSchema, GenericStringEncodingDepthsSchema, DefinedByteSlicesSchema, CustomElementByteSliceSchema, AliasResultMarshalersSchema, UnconstrainedCustomMarshalerPayloadSchema } from './schemas.zod';
import { StringEncodingDepthsSchema as MiniStringEncodingDepthsSchema, GenericStringEncodingDepthsSchema as MiniGenericStringEncodingDepthsSchema, DefinedByteSlicesSchema as MiniDefinedByteSlicesSchema, CustomElementByteSliceSchema as MiniCustomElementByteSliceSchema, AliasResultMarshalersSchema as MiniAliasResultMarshalersSchema, UnconstrainedCustomMarshalerPayloadSchema as MiniUnconstrainedCustomMarshalerPayloadSchema } from './schemas.zod-mini';
import type { StringEncodingDepths, GenericStringEncodingDepths, DefinedByteSlices, CustomElementByteSlice, AliasResultMarshalers, UnconstrainedCustomMarshalerPayload } from './types';

const typedDepths: StringEncodingDepths = { direct: "7", single: "7", double: "7", triple: "7", defined: "7" };
const typedNilDepths: StringEncodingDepths = { ...typedDepths, defined: null };
const typedGenericDepths: GenericStringEncodingDepths = { applied: "7", double: "7", plain: 7 };
const typedGenericNilDepths: GenericStringEncodingDepths = { ...typedGenericDepths, applied: null };
const typedBytes: DefinedByteSlices = { data: [1, 2] };
const typedCustomBytes: CustomElementByteSlice = { data: ["octet"] };
const typedAliasMarshalers: AliasResultMarshalers = { json_value: "json-value", json_pointer: "json-pointer", text_value: "text-value", text_pointer: "text-pointer", v2_json: 0, appended_text: "appended" };
const typedUnconstrained: UnconstrainedCustomMarshalerPayload = { box: { value: "json-value" } };
const depths = JSON.parse(__DEPTH_JSON__);
const nilDepths = JSON.parse(__NIL_DEPTH_JSON__);
const genericDepths = JSON.parse(__GENERIC_DEPTH_JSON__);
const genericNilDepths = JSON.parse(__GENERIC_NIL_DEPTH_JSON__);
const bytes = JSON.parse(__BYTE_JSON__);
const customBytes = JSON.parse(__CUSTOM_BYTE_JSON__);
const aliasMarshalers = JSON.parse(__ALIAS_MARSHALER_JSON__);
const unconstrained = JSON.parse(__UNCONSTRAINED_JSON__);
void [typedDepths, typedNilDepths, typedGenericDepths, typedGenericNilDepths, typedBytes, typedCustomBytes, typedAliasMarshalers, typedUnconstrained];
for (const schema of [StringEncodingDepthsSchema, MiniStringEncodingDepthsSchema]) {
  schema.parse(depths);
  schema.parse(nilDepths);
  if (schema.safeParse({ ...depths, single: 7 }).success) throw new Error('accepted unquoted *int');
  if (schema.safeParse({ ...depths, double: 7 }).success) throw new Error('accepted unquoted **int');
  if (schema.safeParse({ ...depths, defined: 7 }).success) throw new Error('accepted unquoted defined pointer');
}
for (const schema of [GenericStringEncodingDepthsSchema, MiniGenericStringEncodingDepthsSchema]) {
  schema.parse(genericDepths);
  schema.parse(genericNilDepths);
  if (schema.safeParse({ ...genericDepths, applied: 7 }).success) throw new Error('accepted unquoted applied defined pointer');
  if (schema.safeParse({ ...genericDepths, double: 7 }).success) throw new Error('accepted unquoted double pointer');
  if (schema.safeParse({ ...genericDepths, plain: "7" }).success) throw new Error('accepted quoted ordinary pointer');
}
for (const schema of [DefinedByteSlicesSchema, MiniDefinedByteSlicesSchema]) {
  schema.parse(bytes);
  if (schema.safeParse({ data: "AQI=" }).success) throw new Error('accepted []Octet as base64');
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
