import { DeepGenericPointerWrapperSchema, GenericPointerWrapperSchema, RenamedGenericPointerWrapperSchema, ProductiveIndirectPointerAliasSchema, ProductivePointerMapAliasSchema, ProductiveDoublePointerMapAliasSchema, ProductiveGenericPointerMapAliasSchema, ProductiveGenericHiddenMapAliasSchema } from './schemas.zod';
import { DeepGenericPointerWrapperSchema as MiniDeepGenericPointerWrapperSchema, GenericPointerWrapperSchema as MiniGenericPointerWrapperSchema, RenamedGenericPointerWrapperSchema as MiniRenamedGenericPointerWrapperSchema, ProductiveIndirectPointerAliasSchema as MiniProductiveIndirectPointerAliasSchema, ProductivePointerMapAliasSchema as MiniProductivePointerMapAliasSchema, ProductiveDoublePointerMapAliasSchema as MiniProductiveDoublePointerMapAliasSchema, ProductiveGenericPointerMapAliasSchema as MiniProductiveGenericPointerMapAliasSchema, ProductiveGenericHiddenMapAliasSchema as MiniProductiveGenericHiddenMapAliasSchema } from './schemas.zod-mini';
import { z } from 'zod';
import * as mini from 'zod/mini';
GenericPointerWrapperSchema(z.string()).parse("value");
RenamedGenericPointerWrapperSchema(z.string()).parse("value");
DeepGenericPointerWrapperSchema(z.string()).parse("value");
ProductiveIndirectPointerAliasSchema.parse([[null]]);
ProductivePointerMapAliasSchema.parse({ child: null });
ProductiveDoublePointerMapAliasSchema.parse({ child: null });
ProductiveGenericPointerMapAliasSchema(z.string()).parse({ child: null });
ProductiveGenericHiddenMapAliasSchema.parse({ child: [] });
MiniGenericPointerWrapperSchema(mini.string()).parse("value");
MiniRenamedGenericPointerWrapperSchema(mini.string()).parse("value");
MiniDeepGenericPointerWrapperSchema(mini.string()).parse("value");
MiniProductiveIndirectPointerAliasSchema.parse([[null]]);
MiniProductivePointerMapAliasSchema.parse({ child: null });
MiniProductiveDoublePointerMapAliasSchema.parse({ child: null });
MiniProductiveGenericPointerMapAliasSchema(mini.string()).parse({ child: null });
MiniProductiveGenericHiddenMapAliasSchema.parse({ child: [] });
for (const payload of __MAP_PAYLOADS__.map((value) => JSON.parse(value))) {
  ProductivePointerMapAliasSchema.parse(payload);
  MiniProductivePointerMapAliasSchema.parse(payload);
}
if (ProductivePointerMapAliasSchema.safeParse({ child: [] }).success) throw new Error('accepted an array map value');
if (MiniProductivePointerMapAliasSchema.safeParse({ child: [] }).success) throw new Error('accepted an array map value');
