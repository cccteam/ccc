// The @cccteam/resource declaration the next client release publishes: every name the
// generated files import, with ApiDescriptor, ResourceMap and the types they reach
// carrying every block and field today's generator emits (the live routes, the features
// route, the feature gate on a resource's and a method's descriptor entry and on the
// resource, field and method metadata), and the rest reduced to what the generated files
// touch. The compile test type-checks the four emitted client files against it, and the
// descriptor and the resource metadata map taken out of their wrappers and typed directly
// against it, which is where the excess-property check the wrappers give up lives now: a
// field the generator emits that this declaration does not carry fails the generator's
// own tests, so a field the generator learns is a field this file learns in the same
// change.

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
  feature?: string;
}
export interface MethodDescriptor {
  method: Method;
  property: string;
  route: string;
  scope: ScopeKind;
  answers?: boolean;
  statuses?: readonly number[];
  upload?: { maxBytes: number };
  feature?: string;
}
export interface DomainRouteDescriptor {
  segment: string;
  param: string;
}
export interface FeaturesRoute {
  route: string;
}
export interface LiveRoutes {
  renewRoute: string;
  unsubscribeRoute: string;
  tokenRoute: string;
}
export interface ApiDescriptor {
  resources: Record<string, ResourceDescriptor>;
  methods: Record<string, MethodDescriptor>;
  domainRoute?: DomainRouteDescriptor;
  consolidatedRoute?: string;
  permissionDigestRoute: string;
  userDomainsRoute: string;
  features?: FeaturesRoute;
  live?: LiveRoutes;
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
  feature?: string;
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
  feature?: string;
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
  feature?: string;
}
export type Meta = MethodMeta | ResourceMeta;
export type ResourceMap = Record<Resource, ResourceMeta>;
