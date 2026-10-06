// A stand-in for the @cccteam/resource package as released before the descriptor's live
// and features blocks and before the feature gate on its entries and on the metadata:
// every name the generated files import, with ApiDescriptor, ResourceMap and the types
// they reach declared as that release declared them, and the rest reduced to what the
// generated files touch. The compile test type-checks the four emitted client files
// against it to prove that a descriptor and metadata from a newer generator compile
// against an older client, which ignores what it does not know.

export type Brand<K, T> = K & { __brand: T };
export type Permission = Brand<string, 'Permission'>;
export type Resource = Brand<string, 'Resource'>;
export type Domain = Brand<string, 'Domain'>;
export type FieldName = Brand<string, 'FieldName'>;
export type Method = Brand<string, 'Method'>;

export type ScopeKind = 'global' | 'domain';
export type ResourceOperation = 'list' | 'read' | 'create' | 'patch' | 'remove' | 'batch';

export interface PageDescriptor {
  default: number;
  max?: number;
}
export interface OrderDescriptor {
  field: string;
  direction: 'asc' | 'desc';
}
export interface ResourceDescriptor {
  resource: Resource;
  property: string;
  route: string;
  scope: ScopeKind;
  consolidated: boolean;
  keys: readonly string[];
  operations: readonly ResourceOperation[];
  patchable?: readonly string[];
  page?: PageDescriptor;
  order?: readonly OrderDescriptor[];
  files?: readonly string[];
}
export interface MethodDescriptor {
  method: Method;
  property: string;
  route: string;
  scope: ScopeKind;
  answers?: boolean;
  statuses?: readonly number[];
  upload?: { maxBytes: number };
}
export interface DomainRouteDescriptor {
  segment: string;
  param: string;
}
export interface ApiDescriptor {
  resources: Record<string, ResourceDescriptor>;
  methods: Record<string, MethodDescriptor>;
  domainRoute?: DomainRouteDescriptor;
  consolidatedRoute?: string;
  permissionDigestRoute: string;
  userDomainsRoute: string;
}

export interface ClientOptions {
  baseUrl: string;
}
export interface ClientBase {
  readonly descriptor: ApiDescriptor;
  readonly baseUrl: string;
}
export type Client<G, D> = ClientBase & G & { domain(domain: Domain | string): D };
export declare function createClient<G, D>(descriptor: ApiDescriptor, options: ClientOptions): Client<G, D>;

export interface ResourceHandleBase<Row, Key extends unknown[]> {
  readonly resource: Resource;
  readonly descriptor: ResourceDescriptor;
  url(key: Key): string;
  read(key: Key): Promise<Row>;
}
export type ResourceHandle<Row, Key extends unknown[], Ops extends ResourceOperation, Create = never, Patch = never> =
  ResourceHandleBase<Row, Key>
  & ('list' extends Ops ? { list(): Promise<Row[]> } : unknown)
  & ('create' extends Ops ? { create(body: Create): Promise<Row> } : unknown)
  & ('patch' extends Ops ? { patch(key: Key, body: Patch): Promise<Row> } : unknown);
export interface MethodHandle<Body, Result = void> {
  readonly method: Method;
  readonly descriptor: MethodDescriptor;
  execute(body: Body): Promise<Result>;
}
export interface UploadMethodHandle<Body, Result = void> extends MethodHandle<Body, Result> {
  upload(body: Body, files: readonly Blob[]): Promise<Result>;
}
export type NullBoolean = null | true | false;

export type ValidDisplayTypes =
  | 'string' | 'number' | 'boolean' | 'nullboolean' | 'date' | 'civildate' | 'uuid' | 'enumerated' | 'object' | 'bytes'
  | 'string[]' | 'number[]' | 'boolean[]' | 'date[]' | 'civildate[]' | 'uuid[]' | 'object[]' | 'bytes[]';
export type ValidRPCTypes = ValidDisplayTypes;
export interface EnumerationOption {
  id: string;
  display: string;
}
export interface RPCFieldMeta {
  fieldName: string;
  displayType: ValidRPCTypes;
  enumeratedResource?: Resource;
  enumeration?: EnumerationOption[];
}
export interface MethodMeta {
  route: string;
  answers?: true;
  fields: RPCFieldMeta[];
}
export type FilterEligibility = 'always' | 'withIndexed';
export type MaskingBehavior = 'positional';
export interface FieldMeta {
  fieldName: string;
  required: boolean;
  primaryKey?: { ordinalPosition: number };
  displayType: ValidDisplayTypes;
  enumeratedResource?: Resource;
  enumeration?: EnumerationOption[];
  isIndex: boolean;
  filterable?: FilterEligibility;
  masking?: MaskingBehavior;
  maxLength?: number;
  readOnly?: boolean;
  writeOnly?: boolean;
}
export interface ResourceMeta {
  route: string;
  consolidatedRoute?: string;
  rowsOf?: Resource;
  listDisabled?: boolean;
  readDisabled?: boolean;
  createDisabled?: boolean;
  updateDisabled?: boolean;
  deleteDisabled?: boolean;
  fields: FieldMeta[];
}
export type Meta = MethodMeta | ResourceMeta;
export type ResourceMap = Record<Resource, ResourceMeta>;
