import { ProductivePointerMapAliasSchema } from './schemas.zod';
import { ProductivePointerMapAliasSchema as MiniProductivePointerMapAliasSchema } from './schemas.zod-mini';
ProductivePointerMapAliasSchema.parse({ child: null });
MiniProductivePointerMapAliasSchema.parse({ child: null });
