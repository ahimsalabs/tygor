// This file is type-checked to ensure documentation examples are valid.
// Snippets are extracted for README documentation.

import {
  createClient,
  ServerError,
  type FetchFunction,
  type ServiceRegistry,
} from '@tygor/client';

// Mock registry type for documentation examples
type Manifest = {
  'MyService.MyMethod': { req: { param: string }; res: { value: string } };
};

declare const registry: ServiceRegistry<Manifest>;

// [snippet:basic-usage]
const client = createClient(
  registry,
  {
    baseUrl: 'http://localhost:8080',
    headers: () => ({
      'Authorization': 'Bearer my-token'
    })
  }
);

// Type-safe RPC calls
const result = await client.MyService.MyMethod({ param: 'value' });
// [/snippet:basic-usage]

void result;

async function customFetchExample() {
  // [snippet:custom-fetch]
  // Type-safe custom fetch
  const customFetch: FetchFunction = async (url, init) => {
    console.log('Fetching:', url);
    return fetch(url, init);
  };

  const client = createClient(
    registry,
    {
      baseUrl: 'http://localhost:8080',
      fetch: customFetch
    }
  );
  // [/snippet:custom-fetch]

  void client;
}

async function errorHandlingExample() {
  // [snippet:error-handling]
  try {
    await client.MyService.MyMethod({ param: 'value' });
  } catch (err) {
    if (err instanceof ServerError) {
      console.error(err.code);     // e.g., "invalid_argument"
      console.error(err.message);  // Human-readable message
      console.error(err.details);  // Additional error details
    }
  }
  // [/snippet:error-handling]
}

// Keep functions referenced
void customFetchExample;
void errorHandlingExample;
