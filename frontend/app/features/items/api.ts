import type { ExternalStockCheckResult, ExternalStockConfig, ExternalStockHeaderDraft, PublishLocation } from './models';
import {
AccountDetail,
BatchCancelResponse,
BatchIDResponse,
CategoryRecommendationResponse,
Item,
ItemAISettings,
ItemDetailResponse,
ItemStock,
ItemStockUpdate,
ItemPublishBatchPreviewResponse,
ItemPublishBatchResponse,
ItemPublishResponse,ItemSyncResponse,
OperationResponse,
ShippingRule
} from './models';
import { ApiError, type RequestControlOptions } from '../../../shared/http/client';
import { contractClient, contractMultipartBody, runContractRequest } from '../../../shared/api-contract/client';
import { collectionFrom } from '../../../shared/http/contract';
import type { PublishSkuRow, PublishSpec } from '../../../shared/ui/publish/publishSpecs';
export type * from './models';
import { getPublishLocations as queryPublishLocations,type PublishLocationRequestOptions } from './amapLocation';

/** 商品账号筛选器读取非敏感账号摘要。 */
export const getAccountDetails = async (options?: RequestControlOptions): Promise<AccountDetail[]> => runContractRequest(/* signal 控制商品页账号摘要请求的取消和超时。 */ signal => contractClient.GET('/api/v1/accounts/details', { signal }), options);

/** 商品页面读取自动化发货规则的兼容列表。 */
export const getShippingRules = async (options?: RequestControlOptions): Promise<ShippingRule[]> => runContractRequest(/* signal 控制商品页规则读取请求的取消和超时。 */ signal => contractClient.GET('/api/v1/automation-rules', { signal }), options) as unknown as Promise<ShippingRule[]>;

// getPublishLocations 通过 feature API 边界读取地点，并把取消/超时控制传入地图服务。
export const getPublishLocations = (longitude: number, latitude: number, options?: PublishLocationRequestOptions): Promise<PublishLocation[]> => queryPublishLocations(longitude, latitude, options);
export type { PublishLocation } from './models';

// itemErrorMessage 将统一 HTTP 错误码归一为商品 feature 可执行的用户提示。
export const itemErrorMessage = (error: unknown, fallback: string): string => {
  if (error instanceof ApiError) {
    switch (error.code) {
      case 'stock_permission_missing':
        return '发布失败：该账号没有库存发布权限，无法按库存数量发布商品。请换账号或先在闲鱼确认库存能力。';
      case 'conflict':
      case 'item_conflict':
        return '商品状态已发生变化，请刷新后重试。';
      case 'external_result_unknown':
      case 'manual_review_required':
        return '平台结果暂时无法确认，请先人工核对闲鱼状态，再决定是否重试。';
      case 'retryable':
      case 'temporarily_unavailable':
        return '平台暂时不可用，请稍后重试。';
      default:
        return error.message || fallback;
    }
  }
  if (error instanceof Error) return error.message;
  // message 保存兼容 API 错误对象中的文本说明。
  const message = typeof error === 'object' && error !== null && 'message' in error ? (error as { /** message 是兼容错误对象中的文本说明。 */ message?: unknown }).message : undefined;
  return typeof message === 'string' ? message : fallback;
};
// Items
// normalizeBooleanFlag 归一化布尔标记。
const normalizeBooleanFlag = (value: unknown): boolean =>
    value === true || value === 1 || value === '1';

// getItems 读取商品列表。
export const getItems = async (cookieId?: string, options?: RequestControlOptions): Promise<Item[]> => {
    // res 接口响应结果，用于当前 API 处理流程。
    const res = await runContractRequest(/* signal 控制商品列表请求的取消和超时。 */ signal => contractClient.GET('/api/v1/items', { params: { query: { cookie_id: cookieId } }, signal }), options) as unknown;
    // items 商品列表，用于当前 API 处理流程。
    const items = collectionFrom<Item>(res, ['items', 'data', 'results']);
    return items.map(/* 当前回调用于处理集合元素或接口响应。 */ (item: any) => ({
      ...item,
      id: item.id || `${item.cookie_id}-${item.item_id}`,
      is_multi_spec: normalizeBooleanFlag(item.is_multi_spec),
      is_multi_qty_ship: normalizeBooleanFlag(item.is_multi_qty_ship ?? item.multi_quantity_delivery),
      multi_quantity_delivery: normalizeBooleanFlag(item.multi_quantity_delivery ?? item.is_multi_qty_ship),
    }));
}

// getItemAISettings 在用户打开配置弹窗时读取单件商品的完整 AI 配置。
export const getItemAISettings = async (cookieId: string, itemId: string, options?: RequestControlOptions): Promise<ItemAISettings> => {
    // response 是生成契约约束的商品级 AI 配置传输对象。
    const response = await runContractRequest(/* signal 控制商品级 AI 配置读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items/{cookie_id}/{item_id}/ai-settings', {
      params: { path: { cookie_id: cookieId, item_id: itemId } },
      signal,
    }), options);
    return { ai_override: response.ai_override, item_context: response.item_context };
}

/** ExternalStockHeaderTransport 是接口返回的请求头名称，不含明文值。 */
interface ExternalStockHeaderTransport {
  /** 请求头名称。 */
  name: string;
  /** 服务端是否保存了值。 */
  set: boolean;
}

/** ExternalStockTransport 是外部库存接口返回的非秘密字段。 */
interface ExternalStockTransport {
  /** 是否启用。 */
  enabled: boolean;
  /** 外部库存地址。 */
  source_url: string;
  /** 可用库存路径。 */
  json_path: string;
  /** 超时秒数。 */
  timeout_seconds: number;
  /** 自定义 User-Agent。 */
  user_agent: string;
  /** 五段 cron。 */
  cron: string;
  /** IANA 时区。 */
  timezone: string;
  /** 周期分钟数。 */
  interval_minutes: number;
  /** 是否演练。 */
  dry_run: boolean;
  /** 是否同步平台库存。 */
  sync_platform_stock: boolean;
  /** 库存上限。 */
  stock_cap: number;
  /** 已保存请求头名称。 */
  headers: ExternalStockHeaderTransport[];
  /** 上次检查时间。 */
  last_checked_unix: number;
  /** 上次可用库存。 */
  last_available?: number | null;
  /** 上次动作。 */
  last_action: string;
  /** 上次状态。 */
  last_status: string;
  /** 上次错误。 */
  last_error: string;
  /** 退避结束时间。 */
  backoff_until_unix: number;
  /** 退避类别。 */
  backoff_class: string;
}

// externalStockFromResponse 把契约响应转换成不含请求头明文的界面模型。
const externalStockFromResponse = (response: ExternalStockTransport): ExternalStockConfig => ({
  enabled: response.enabled,
  sourceUrl: response.source_url,
  jsonPath: response.json_path,
  timeoutSeconds: response.timeout_seconds,
  userAgent: response.user_agent,
  cron: response.cron,
  timezone: response.timezone,
  intervalMinutes: response.interval_minutes,
  dryRun: response.dry_run,
  syncPlatformStock: response.sync_platform_stock,
  stockCap: response.stock_cap,
  headers: response.headers.map(/* header 是只含名称的已保存请求头。 */ header => ({ name: header.name, set: header.set })),
  lastCheckedUnix: response.last_checked_unix,
  lastAvailable: response.last_available ?? null,
  lastAction: response.last_action,
  lastStatus: response.last_status,
  lastError: response.last_error,
  backoffUntilUnix: response.backoff_until_unix,
  backoffClass: response.backoff_class,
});

// getItemExternalStock 读取商品外部库存配置。响应不包含请求头明文。
export const getItemExternalStock = async (cookieId: string, itemId: string, options?: RequestControlOptions): Promise<ExternalStockConfig> => {
  // response 是生成契约约束的外部库存配置。
  const response = await runContractRequest(/* signal 控制外部库存配置读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items/{cookie_id}/{item_id}/external-stock', {
    params: { path: { cookie_id: cookieId, item_id: itemId } },
    signal,
  }), options);
  return externalStockFromResponse(response);
};

// updateItemExternalStock 保存配置。请求头值只在这次请求里发送，返回结果仍然不含明文。
export const updateItemExternalStock = async (cookieId: string, itemId: string, config: ExternalStockConfig, headers: ExternalStockHeaderDraft[], options?: RequestControlOptions): Promise<ExternalStockConfig> => {
  // response 是保存后的非秘密配置。
  const response = await runContractRequest(/* signal 控制外部库存配置保存的取消和超时。 */ signal => contractClient.PUT('/api/v1/items/{cookie_id}/{item_id}/external-stock', {
    params: { path: { cookie_id: cookieId, item_id: itemId } },
    body: {
      enabled: config.enabled,
      source_url: config.sourceUrl,
      json_path: config.jsonPath,
      timeout_seconds: config.timeoutSeconds,
      user_agent: config.userAgent,
      cron: config.cron,
      timezone: config.timezone,
      interval_minutes: config.intervalMinutes,
      dry_run: config.dryRun,
      sync_platform_stock: config.syncPlatformStock,
      stock_cap: config.stockCap,
      headers: headers.map(/* header 是本次要保存的一条请求头草稿。 */ header => ({ name: header.name, value: header.value, keep: header.keep })),
    },
    signal,
  }), options);
  return externalStockFromResponse(response);
};

// checkItemExternalStock 立即检查商品。dryRun 为真时只记录将要执行的动作。
export const checkItemExternalStock = async (cookieId: string, itemId: string, dryRun: boolean, options?: RequestControlOptions): Promise<ExternalStockCheckResult> => {
  // response 是一次检查的非秘密结果。
  const response = await runContractRequest(/* signal 控制立即检查的取消和超时。 */ signal => contractClient.POST('/api/v1/items/{cookie_id}/{item_id}/external-stock/check', {
    params: { path: { cookie_id: cookieId, item_id: itemId } },
    body: { dry_run: dryRun },
    signal,
  }), { timeoutMs: 90_000, ...options });
  return {
    action: response.action,
    status: response.status,
    available: response.available ?? null,
    message: response.message,
    dryRun: response.dry_run,
    listingState: response.listing_state,
    config: externalStockFromResponse(response.config),
  };
};

// updateItemAISettings 保存单件商品的 AI 覆盖状态和专属资料。
export const updateItemAISettings = async (cookieId: string, itemId: string, settings: ItemAISettings, options?: RequestControlOptions): Promise<ItemAISettings> => {
    // response 是服务端保存后返回的规范化商品级 AI 配置。
    const response = await runContractRequest(/* signal 控制商品级 AI 配置保存的取消和超时。 */ signal => contractClient.PUT('/api/v1/items/{cookie_id}/{item_id}/ai-settings', {
      params: { path: { cookie_id: cookieId, item_id: itemId } },
      body: settings,
      signal,
    }), options);
    return { ai_override: response.ai_override, item_context: response.item_context };
}

// getItemStock 在用户打开库存弹窗时读取商品当前库存（一次平台请求，不在列表加载时自动调用）。
export const getItemStock = async (cookieId: string, itemId: string, options?: RequestControlOptions): Promise<ItemStock> => {
    // response 是生成契约约束的库存传输对象。
    const response = await runContractRequest(/* signal 控制库存读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items/{cookie_id}/{item_id}/stock', {
      params: { path: { cookie_id: cookieId, item_id: itemId } },
      signal,
    }), options);
    return {
      itemId: response.data.item_id,
      multiSpec: response.data.multi_spec,
      quantity: response.data.quantity,
      skus: response.data.skus.map(/* sku 是库存接口返回的一行规格。 */ sku => ({ key: sku.key, label: sku.label, priceCents: sku.price_cents, quantity: sku.quantity })),
    };
}

// updateItemStock 提交库存修改（服务端先读后写，两次平台请求，失败不重试）。
export const updateItemStock = async (cookieId: string, itemId: string, update: ItemStockUpdate, options?: RequestControlOptions): Promise<void> => {
    await runContractRequest(/* signal 控制库存修改的取消和超时。 */ signal => contractClient.PUT('/api/v1/items/{cookie_id}/{item_id}/stock', {
      params: { path: { cookie_id: cookieId, item_id: itemId } },
      body: {
        quantity: update.quantity,
        expected_quantity: update.expectedQuantity,
        skus: update.skus?.map(/* row 是需要提交的一行规格库存修改。 */ row => ({ key: row.key, quantity: row.quantity, expected_quantity: row.expectedQuantity })),
      },
      signal,
    }), options);
}

// isItemStockChangedError 判断库存修改是否因平台库存已变化而被拒绝，此时界面应要求用户重新读取。
export const isItemStockChangedError = (error: unknown): boolean => error instanceof ApiError && error.code === 'item_stock_changed';

// syncItemsFromAccount 从账号同步商品。
export const syncItemsFromAccount = async (cookieId: string): Promise<ItemSyncResponse> => {
    return runContractRequest(/* signal 控制商品同步请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/get-all-from-account', { body: { cookie_id: cookieId } as never, signal })) as unknown as Promise<ItemSyncResponse>;
}

// deleteItem 删除商品。
export const deleteItem = async (cookieId: string, itemId: string): Promise<OperationResponse> => {
    return runContractRequest(/* signal 控制商品删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/items/{cookie_id}/{item_id}', { params: { path: { cookie_id: cookieId, item_id: itemId } }, signal }));
}

// createItem 创建商品。
export const createItem = async (cookieId: string, data: Partial<Item>): Promise<OperationResponse> => {
    return runContractRequest(/* signal 控制商品创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/{cookie_id}', { params: { path: { cookie_id: cookieId } }, body: data as never, signal }));
}

// publishItem 发布商品。
export const publishItem = async (form: {
    /** cookie_id 表示登录凭证标识。 */ cookie_id: string;
    /** title 表示标题。 */ title: string;
    /** description 表示描述。 */ description: string;
    /** price 表示售价。 */ price: string;
    /** original_price 表示原始售价。 */ original_price?: string;
    /** quantity 表示待发布商品的件数，提交前会转换为表单字符串。 */ quantity: string | number;
    /** postage_mode 表示运费模式。 */ postage_mode: string;
    /** postage 表示运费。 */ postage?: string;
	/** images 表示图片列表。 */ images: File[];
	/** specs 表示商品规格维度及其规格值。 */ specs?: PublishSpec[];
	/** skuRows 表示规格组合的价格和库存。 */ skuRows?: PublishSkuRow[];
	/** location 表示地址。 */ location?: PublishLocation;
	/** category 表示用户选中的闲鱼类目；省略时由后端自动识别并使用电子资料兜底。 */ category?: {
	  /** cat_id 表示闲鱼类目主键。 */ cat_id: string;
	  /** cat_name 表示闲鱼类目名称。 */ cat_name: string;
	  /** channel_cat_id 表示闲鱼频道类目主键。 */ channel_cat_id?: string;
	  /** tb_cat_id 表示可选淘宝类目主键。 */ tb_cat_id?: string;
	};
}): Promise<ItemPublishResponse> => {
    // body 请求体，用于当前 API 处理流程。
    const body = new FormData();
    body.set('cookie_id', form.cookie_id);
    body.set('title', form.title);
    body.set('description', form.description);
    body.set('price', form.price);
    body.set('original_price', form.original_price || '');
    body.set('quantity', String(form.quantity));
    body.set('postage_mode', form.postage_mode);
    body.set('postage', form.postage || '');
	if (form.location) body.set('location', JSON.stringify(form.location));
    if (form.category) {
	  body.set('category_id', form.category.cat_id);
	  body.set('category_name', form.category.cat_name);
	  body.set('channel_category_id', form.category.channel_cat_id || '');
	  body.set('tb_category_id', form.category.tb_cat_id || '');
	}
    if (form.specs?.length && form.skuRows?.length) {
      // specImages 保存规格值图片，并按下标与 itemProperties 中的 image_index 对应。
      const specImages: File[] = [];
      // itemProperties 保存与闲鱼官方发布请求一致的规格维度和规格值结构。
      const itemProperties = form.specs.map(/* spec 转换为官方规格维度 DTO。 */ spec => ({
        propertyName: spec.name.trim(),
        supportImage: spec.supportImage,
        propertyValues: spec.values.filter(/* value 筛选非空规格值。 */ value => value.value.trim()).map(/* value 转换为官方规格值 DTO。 */ value => {
          // imageIndex 表示该规格值图片在 spec_images multipart 字段中的位置。
          const imageIndex = spec.supportImage && value.image ? specImages.push(value.image) - 1 : -1;
          return { propertyValue: value.value.trim(), ...(imageIndex >= 0 ? { image_index: imageIndex } : {}) };
        }),
      }));
      // itemSkuList 保存每个组合的官方 propertyList、price 和 quantity 字段。
      const itemSkuList = form.skuRows.map(/* row 转换为官方 SKU 行 DTO。 */ row => ({
        price: row.price,
        quantity: Number(row.quantity),
        propertyList: row.valueLabels.map(/* valueText、index 组成当前 SKU 的规格对。 */ (valueText, index) => ({ propertyText: form.specs?.[index]?.name.trim() || '', valueText })),
      }));
      body.set('item_properties', JSON.stringify(itemProperties));
      body.set('item_sku_list', JSON.stringify(itemSkuList));
      // specImage 表示当前待上传到规格图片字段的文件。
      for (const /* specImage 表示当前待上传到规格图片字段的文件。 */ specImage of specImages) body.append('spec_images', specImage);
	}
    for (const // file 上传文件，用于当前 API 处理流程。
file of form.images) {
      body.append('images', file);
    }
    return runContractRequest(/* signal 控制商品发布上传请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish', { body: contractMultipartBody(body), signal })) as unknown as Promise<ItemPublishResponse>;
}

// recommendPublishCategory 推荐商品发布分类。
export const recommendPublishCategory = async (cookieId: string, keyword: string, options?: RequestControlOptions): Promise<CategoryRecommendationResponse> => {
    // 类目推荐成功响应使用共享 CategoryRecommendationResponse。
    // category 字段保留平台类目 ID、名称和频道类目 ID。
    // tb_cat_id 继续保持可选，兼容电子资料类目。
    // 请求仍携带账号 ID 和关键词。
    // 失败响应由共享 HTTP 错误结构处理。
    // 该类型收口不改变凭证刷新和错误处理。
    // 前端批量发布流程可直接复用 category。
    // 旧路径继续由现有 Vite 代理转发。
	return runContractRequest(/* signal 控制商品类目推荐请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-categories/recommend', { body: { cookie_id: cookieId, keyword } as never, signal }), options) as unknown as Promise<CategoryRecommendationResponse>;
};

// previewItemPublishBatch 预览商品批量发布。
export const previewItemPublishBatch = async (form: {
    /** file 表示上传文件。 */ file: File;
    /** imagesZip 表示图片压缩包。 */ imagesZip?: File | null;
    /** defaultCookieId 表示默认账号凭证标识。 */ defaultCookieId?: string;
    /** fallbackCategory 表示备用分类。 */ fallbackCategory: {
      /** catId 表示分类标识。 */ catId: string;
      /** catName 表示分类名称。 */ catName: string;
      /** channelCatId 表示渠道分类标识。 */ channelCatId?: string;
      /** tbCatId 表示淘宝分类标识。 */ tbCatId?: string;
    };
	/** location 表示地址。 */ location?: PublishLocation;
	/** publishIntervalSeconds 表示最终商品发布之间的最小间隔秒数。 */
	publishIntervalSeconds?: number;
}, options?: RequestControlOptions): Promise<ItemPublishBatchPreviewResponse> => {
    // body 请求体，用于当前 API 处理流程。
    const body = new FormData();
    body.set('file', form.file);
    if (form.imagesZip) body.set('images_zip', form.imagesZip);
    if (form.defaultCookieId) body.set('default_cookie_id', form.defaultCookieId);
    body.set('fallback_category_id', form.fallbackCategory.catId);
    body.set('fallback_category_name', form.fallbackCategory.catName);
    body.set('fallback_channel_category_id', form.fallbackCategory.channelCatId || '');
    body.set('fallback_tb_category_id', form.fallbackCategory.tbCatId || '');
	if (form.location) body.set('location', JSON.stringify(form.location));
	body.set('publish_interval_seconds', String(form.publishIntervalSeconds ?? 5));
	return runContractRequest(/* signal 控制批量发布预览上传的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-batches/preview', { body: contractMultipartBody(body), signal }), options) as unknown as Promise<ItemPublishBatchPreviewResponse>;
}

// startItemPublishBatch 启动商品批量发布。
export const startItemPublishBatch = async (previewId: string, options?: RequestControlOptions): Promise<BatchIDResponse> => {
	return runContractRequest(/* signal 控制批量发布启动请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-batches', { body: { preview_id: previewId } as never, signal }), options) as unknown as Promise<BatchIDResponse>;
}

// getItemPublishBatch 读取商品发布批次。
export const getItemPublishBatch = async (batchId: string, options?: RequestControlOptions): Promise<ItemPublishBatchResponse> => {
	return runContractRequest(/* signal 控制批量发布状态读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items/publish-batches/{batch_id}', { params: { path: { batch_id: batchId } }, signal }), options) as unknown as Promise<ItemPublishBatchResponse>;
}

// getItemPublishBatches 读取商品发布批次列表。
export const getItemPublishBatches = async (limit = 20, options?: RequestControlOptions): Promise<ItemPublishBatchResponse[]> => {
    // res 接口响应结果，用于当前 API 处理流程。
    const res = await runContractRequest(/* signal 控制批量发布列表读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items/publish-batches', { params: { query: { limit } }, signal }), options) as unknown;
    return collectionFrom<ItemPublishBatchResponse>(res, ['batches', 'data', 'items']);
}

// deleteItemPublishBatch 删除商品发布批次。
export const deleteItemPublishBatch = async (batchId: string, options?: RequestControlOptions): Promise<OperationResponse> => {
	return runContractRequest(/* signal 控制批量发布删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/items/publish-batches/{batch_id}', { params: { path: { batch_id: batchId } }, signal }), options);
}

// cancelItemPublishBatch 取消商品发布批次。
export const cancelItemPublishBatch = async (batchId: string, options?: RequestControlOptions): Promise<BatchCancelResponse> => {
	return runContractRequest(/* signal 控制批量发布取消请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-batches/{batch_id}/cancel', { params: { path: { batch_id: batchId } }, body: {} as never, signal }), options) as unknown as Promise<BatchCancelResponse>;
}

// retryFailedItemPublishBatch 重试失败的商品发布任务。
export const retryFailedItemPublishBatch = async (batchId: string, options?: RequestControlOptions): Promise<BatchIDResponse> => {
	return runContractRequest(/* signal 控制批量发布重试请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-batches/{batch_id}/retry-failed', { params: { path: { batch_id: batchId } }, body: {} as never, signal }), options) as unknown as Promise<BatchIDResponse>;
}

// updateItem 更新商品。
export const updateItem = async (cookieId: string, itemId: string, data: Partial<Item>): Promise<OperationResponse> => {
    return runContractRequest(/* signal 控制商品更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/items/{cookie_id}/{item_id}', { params: { path: { cookie_id: cookieId, item_id: itemId } }, body: data as never, signal }));
}

/** 读取指定账号与商品的详情，用于编辑器恢复表单，并将生成传输 DTO 保持在 feature adapter 边界。 */
export const getItemDetail = async (accountID: string, itemID: string): Promise<ItemDetailResponse> => {
  // response 保存已由 OpenAPI 路径类型约束的商品详情传输响应。
  const response = await runContractRequest(/* signal 控制商品详情请求的取消和超时。 */ signal => contractClient.GET('/api/v1/items/{cookie_id}/{item_id}', { params: { path: { cookie_id: accountID, item_id: itemID } }, signal }));
  return response;
};
