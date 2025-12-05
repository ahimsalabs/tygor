import { createClient, type ServiceRegistry } from "@tygor/client";

// Mock registry for type checking
type Manifest = {
  "Users.Get": { req: { id: string }; res: { id: number; name: string } };
  "Users.Create": { req: { name: string }; res: { id: number; name: string } };
};

declare const registry: ServiceRegistry<Manifest>;

// [snippet:client-usage]
const client = createClient(registry, {
  baseUrl: "http://localhost:8080",
});

const user = await client.Users.Get({ id: "123" });
// [/snippet:client-usage]

void user;

async function clientCallExample() {
  // [snippet:client-call]
  const user = await client.Users.Get({ id: "123" });
  // user: User (autocomplete works)
  // [/snippet:client-call]
  void user;
}

void clientCallExample;
