// Re-export generated schema types
export type {
  DiscoverySchema,
  TypeDescriptor,
  TypeRef,
  FieldDescriptor,
  ServiceDescriptor,
  EndpointDescriptor,
  GoIdentifier,
} from "../devserver/types_tygor_dev_cmd_tygor_internal_dev";

/** RPC error reported by @tygor/client */
export interface TygorRpcError {
  service: string;
  method: string;
  code: string;
  message: string;
  timestamp: number;
}
