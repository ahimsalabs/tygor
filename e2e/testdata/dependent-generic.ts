import { z } from 'zod';
import * as mini from 'zod/mini';
import { DependentContainerSchema, ForwardedDependentWireSchema } from './schemas.zod';
import { DependentContainerSchema as MiniDependentContainerSchema, ForwardedDependentWireSchema as MiniForwardedDependentWireSchema } from './schemas.zod-mini';
import type { DependentContainer, ForwardedDependentWire } from './types';

const typed: DependentContainer<string, string[]> = { values: ["value"] };
const forwarded: ForwardedDependentWire<string> = { box: { value: 1 } };
void [typed, forwarded];
DependentContainerSchema(z.string(), z.array(z.string()).nullable()).parse({ values: ["value"] });
DependentContainerSchema(z.string(), z.array(z.string()).nullable()).parse({ values: null });
MiniDependentContainerSchema(mini.string(), mini.nullable(mini.array(mini.string()))).parse({ values: ["value"] });
MiniDependentContainerSchema(mini.string(), mini.nullable(mini.array(mini.string()))).parse({ values: null });
ForwardedDependentWireSchema(z.string()).parse({ box: { value: 1 } });
MiniForwardedDependentWireSchema(mini.string()).parse({ box: { value: 1 } });
