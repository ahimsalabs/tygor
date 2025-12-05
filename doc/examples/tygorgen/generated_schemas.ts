import { z } from "zod";

// [snippet:zod-output]
export const UserSchema = z.object({
  id: z.number().int(),
  name: z.string().min(1).min(2),
  email: z.string().min(1).email(),
  avatar: z.nullable(z.string()),
  created_at: z.string().datetime(),
});
// [/snippet:zod-output]
