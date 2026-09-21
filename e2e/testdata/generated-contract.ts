import { ASchema, AboveCycleSchema, ContractSchema, DerivedSchema, NodeSchema, PairSchema, RecursiveListSchema, RecursiveMapSchema, TreeSchema } from './schemas.zod';
import { ASchema as MiniASchema, AboveCycleSchema as MiniAboveCycleSchema, ContractSchema as MiniContractSchema, DerivedSchema as MiniDerivedSchema, NodeSchema as MiniNodeSchema, PairSchema as MiniPairSchema, RecursiveListSchema as MiniRecursiveListSchema, RecursiveMapSchema as MiniRecursiveMapSchema, TreeSchema as MiniTreeSchema } from './schemas.zod-mini';
import { schemaMap } from './schemas.map.zod';
import { schemaMap as miniSchemaMap } from './schemas.map.zod-mini';
import type { Contract } from './types';

const valid = JSON.parse(__WIRE_JSON__);
const nullCollections: Pick<Contract, 'items' | 'counts' | 'lookup' | 'groups' | 'bytes' | 'byte_groups' | 'names' | 'labels' | 'optional' | 'maybe'> = {
  items: null,
  counts: null,
  lookup: null,
  groups: null,
  bytes: null,
  byte_groups: null,
  names: null,
  labels: null,
  optional: null,
  maybe: null,
};
const pointerElements: Contract['items'] = [null];
const nestedNilSlice: Contract['groups'] = [null];
void [nullCollections, pointerElements, nestedNilSlice];
for (const schema of [ContractSchema, MiniContractSchema]) {
  schema.parse(valid);
  schema.parse({ ...valid, optional: null });
  schema.parse({ ...valid, ...nullCollections });
  if (schema.safeParse({ ...valid, flag: "garbage" }).success) throw new Error('accepted invalid bool string');
  if (schema.safeParse({ ...valid, flag: "true" }).success) throw new Error('ignored bool oneof');
  if (schema.safeParse({ ...valid, big_id: "9223372036854775808" }).success) throw new Error('accepted overflowing int64');
  if (schema.safeParse({ ...valid, unsigned_id: "18446744073709551616" }).success) throw new Error('accepted overflowing uint64');
  if (schema.safeParse({ ...valid, ratio: "NaN" }).success) throw new Error('accepted non-JSON float string');
  if (schema.safeParse({ ...valid, choice: 3 }).success) throw new Error('accepted invalid numeric oneof');
  if (schema.safeParse({ ...valid, exact: 4 }).success) throw new Error('ignored numeric len equality');
  if (schema.safeParse({ ...valid, counts: { nope: "bad" } }).success) throw new Error('accepted invalid numeric map key');
  schema.parse({ ...valid, enum_counts: { "+01": "one", "2": "non-member", "-0": "zero" } });
  schema.parse({ ...valid, alias_enum_counts: { "2": "non-member" } });
  for (const key of ["nope", "1.0", "1e2", "0x10", " 1", "1\n"]) {
    if (schema.safeParse({ ...valid, enum_counts: { [key]: "bad" } }).success) throw new Error('accepted invalid enum map key: ' + JSON.stringify(key));
  }
  if (schema.safeParse({ ...valid, unsigned_counts: { "+1": "bad" } }).success) throw new Error('accepted signed unsigned map key');
  if (schema.safeParse({ ...valid, unsigned_counts: { "256": "bad" } }).success) throw new Error('accepted overflowing uint8 map key');
  if (schema.safeParse({ ...valid, encoded: JSON.stringify("not-an-email") }).success) throw new Error('validated encoded source text instead of its Go string value');
  if (schema.safeParse({ ...valid, encoded_choice: JSON.stringify("green") }).success) throw new Error('ignored encoded string oneof');
  if (schema.safeParse({ ...valid, status: "2" }).success) throw new Error('accepted invalid encoded integer enum');
  if (schema.safeParse({ ...valid, status: "3" }).success) throw new Error('ignored encoded numeric enum validation');
  if (schema.safeParse({ ...valid, status_number: 2 }).success) throw new Error('ignored numeric enum oneof');
  if (schema.safeParse({ ...valid, mode: "green" }).success) throw new Error('ignored string enum oneof');
  if (schema.safeParse({ ...valid, names: [] }).success) throw new Error('ignored pointer-to-named-slice validation');
  if (schema.safeParse({ ...valid, labels: {} }).success) throw new Error('ignored pointer-to-named-map validation');
  if (schema.safeParse({ ...valid, empty_array: ["extra"] }).success) throw new Error('accepted non-empty [0] array');
  if (schema.safeParse({ ...valid, pair_array: [null] }).success) throw new Error('accepted wrong fixed-array length');
}
NodeSchema.parse({ value: "root", next: { value: "leaf", next: null } });
MiniNodeSchema.parse({ value: "root", next: { value: "leaf", next: null } });
ASchema.parse({ b: { a: null } });
MiniASchema.parse({ b: { a: null } });
AboveCycleSchema.parse({ holder: { cycle: { b: { a: null } } } });
MiniAboveCycleSchema.parse({ holder: { cycle: { b: { a: null } } } });
PairSchema.parse({ left: { left: "left" }, right: { right: "right" } });
MiniPairSchema.parse({ left: { left: "left" }, right: { right: "right" } });
DerivedSchema.parse({ base: "base", own: "own" });
MiniDerivedSchema.parse({ base: "base", own: "own" });
TreeSchema((await import('zod')).z.string()).parse({ value: "root", child: { value: "leaf", child: null } });
MiniTreeSchema((await import('zod/mini')).string()).parse({ value: "root", child: { value: "leaf", child: null } });
RecursiveListSchema.parse([[]]);
MiniRecursiveListSchema.parse([[]]);
RecursiveMapSchema.parse({ child: {} });
MiniRecursiveMapSchema.parse({ child: {} });
schemaMap["Contracts.Get"].response.parse(null);
miniSchemaMap["Contracts.Get"].response.parse(null);
schemaMap["Contracts.Walk"].request.parse({ child: {} });
schemaMap["Contracts.Walk"].response.parse([[]]);
miniSchemaMap["Contracts.Walk"].request.parse({ child: {} });
miniSchemaMap["Contracts.Walk"].response.parse([[]]);
