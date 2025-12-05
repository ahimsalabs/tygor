# @tygor/client

TypeScript runtime client for the [tygor](https://tygor.dev) RPC framework.

## Installation

```bash
npm install @tygor/client
```

## Usage

This package provides the runtime client used with tygor-generated TypeScript types and service registry manifest.

### Example

```typescript
import { createClient } from '@tygor/client';
import { registry } from './generated/manifest';
```

<!-- [snippet:doc/examples/client:basic-usage] -->
```typescript title="usage.ts"
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
```
<!-- [/snippet:doc/examples/client:basic-usage] -->

### Custom fetch Implementation

You can provide a custom `fetch` implementation for testing, adding retry logic, or supporting custom environments:

```typescript
import { createClient, type FetchFunction } from '@tygor/client';
```

<!-- [snippet:doc/examples/client:custom-fetch] -->
```typescript title="usage.ts"
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
```
<!-- [/snippet:doc/examples/client:custom-fetch] -->

This is particularly useful for:
- **Testing**: Pass a mock fetch function without type casting
- **Middleware**: Add logging, retry logic, or request/response transformation
- **Custom runtimes**: Support environments without global fetch

## Features

- **Lightweight**: Uses JavaScript Proxies for minimal bundle size
- **Type-safe**: Full TypeScript support with generated types
- **Simple**: Clean, idiomatic API with no code generation bloat
- **Modern**: Built on native `fetch` API

## Error Handling

The client throws `ServerError` instances for failed requests:

```typescript
import { ServerError } from '@tygor/client';
```

<!-- [snippet:doc/examples/client:error-handling] -->
```typescript title="usage.ts"
try {
  await client.MyService.MyMethod({ param: 'value' });
} catch (err) {
  if (err instanceof ServerError) {
    console.error(err.code);     // e.g., "invalid_argument"
    console.error(err.message);  // Human-readable message
    console.error(err.details);  // Additional error details
  }
}
```
<!-- [/snippet:doc/examples/client:error-handling] -->

## Documentation

For full documentation, see the [tygor repository](https://tygor.dev).

## License

MIT
