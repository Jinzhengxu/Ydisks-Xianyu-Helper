// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { ExternalStockConfig, Item } from './api';
import { checkItemExternalStock, getItemExternalStock, updateItemExternalStock } from './api';
import { useExternalStockEditor } from './useExternalStockEditor';

vi.mock('./api', /* externalStockApiMock 替换外部库存接口。 */ () => ({
  getItemExternalStock: vi.fn(),
  updateItemExternalStock: vi.fn(),
  checkItemExternalStock: vi.fn(),
  itemErrorMessage: /* errorMessage 把异常转成界面文案。 */ (error: unknown, fallback: string) => (error instanceof Error ? error.message : fallback),
}));

// item 是弹窗测试使用的商品。
const item = { cookie_id: 'acc', item_id: 'item-1', item_title: '游泳票' } as Item;

// config 是不含请求头明文的配置样本。
const config: ExternalStockConfig = {
  enabled: true, sourceUrl: 'https://stock.example/slots', jsonPath: 'available', timeoutSeconds: 20, userAgent: '',
  cron: '1 0 * * *', timezone: 'Asia/Shanghai', intervalMinutes: 30, dryRun: false, syncPlatformStock: true, stockCap: 1,
  headers: [{ name: 'Authorization', set: true }], lastCheckedUnix: 0, lastAvailable: null, lastAction: '', lastStatus: '',
  lastError: '', backoffUntilUnix: 0, backoffClass: '',
};

describe('useExternalStockEditor', /* suite 验证读取、保存和演练不会回显请求头。 */ () => {
  beforeEach(/* reset 清空接口替身。 */ () => { vi.resetAllMocks(); });

  test('读取后请求头值为空并标记保留', /* loadCase 确认秘密不会回到输入框。 */ async () => {
    vi.mocked(getItemExternalStock).mockResolvedValue(config);
    // hook 是打开弹窗后的编辑器。
    const hook = renderHook(/* renderEditor 渲染外部库存编辑器。 */ () => useExternalStockEditor(item, true));
    await waitFor(/* loaded 等待配置出现。 */ () => expect(hook.result.current.config?.sourceUrl).toBe('https://stock.example/slots'));
    expect(hook.result.current.headers).toEqual([{ name: 'Authorization', value: '', keep: true }]);
    expect(JSON.stringify(hook.result.current.headers)).not.toContain('Bearer');
  });

  test('演练只请求 dry_run，失败状态显示错误', /* checkCase 验证立即演练和失败文案。 */ async () => {
    vi.mocked(getItemExternalStock).mockResolvedValue(config);
    vi.mocked(checkItemExternalStock).mockResolvedValue({
      action: 'stop', status: 'backed_off', available: 1, message: '该商品不支持网页端编辑或重新上架', dryRun: true, listingState: 'off_sale', config,
    });
    // hook 是已加载配置的编辑器。
    const hook = renderHook(/* renderEditor 渲染外部库存编辑器。 */ () => useExternalStockEditor(item, true));
    await waitFor(/* loaded 等待配置出现。 */ () => expect(hook.result.current.config).not.toBeNull());
    // runDryCheck 执行一次演练检查。
    const runDryCheck = async () => { await hook.result.current.checkNow('preview'); };
    await act(runDryCheck);
    expect(checkItemExternalStock).toHaveBeenCalledWith('acc', 'item-1', 'preview');
    expect(hook.result.current.error).toContain('不支持网页端编辑');
  });

  test('确认后才发送一次真实执行，已保存演练保持开启', /* executeCase 验证执行一次需要确认且不改演练开关。 */ async () => {
    vi.mocked(getItemExternalStock).mockResolvedValue({ ...config, dryRun: true });
    vi.mocked(checkItemExternalStock).mockResolvedValue({
      action: 'relist', status: 'ok', available: 1, message: '已重新上架', dryRun: false, listingState: 'on_sale', config: { ...config, dryRun: true },
    });
    // hook 是已加载且演练开启的编辑器。
    const hook = renderHook(/* renderEditor 渲染外部库存编辑器。 */ () => useExternalStockEditor(item, true));
    await waitFor(/* loaded 等待配置出现。 */ () => expect(hook.result.current.config?.dryRun).toBe(true));
    await act(/* armOnly 只打开确认。 */ async () => { hook.result.current.armExecuteOnce(); });
    expect(hook.result.current.executeArmed).toBe(true);
    expect(checkItemExternalStock).not.toHaveBeenCalled();
    await act(/* cancelArmed 取消确认。 */ async () => { hook.result.current.cancelExecuteOnce(); });
    expect(checkItemExternalStock).not.toHaveBeenCalled();
    await act(/* armAgain 再次打开确认。 */ async () => { hook.result.current.armExecuteOnce(); });
    // confirmOnce 确认后发送真实执行。
    const confirmOnce = async () => { await hook.result.current.confirmExecuteOnce(); };
    await act(confirmOnce);
    expect(checkItemExternalStock).toHaveBeenCalledWith('acc', 'item-1', 'execute_once');
    expect(hook.result.current.config?.dryRun).toBe(true);
    expect(updateItemExternalStock).not.toHaveBeenCalled();
  });

  test('保存后清空请求头输入', /* saveCase 验证保存成功后的草稿。 */ async () => {
    vi.mocked(getItemExternalStock).mockResolvedValue(config);
    vi.mocked(updateItemExternalStock).mockResolvedValue(config);
    // hook 是已加载配置的编辑器。
    const hook = renderHook(/* renderEditor 渲染外部库存编辑器。 */ () => useExternalStockEditor(item, true));
    await waitFor(/* loaded 等待配置出现。 */ () => expect(hook.result.current.config).not.toBeNull());
    // runSave 保存当前外部库存草稿。
    const runSave = async () => { await hook.result.current.save(); };
    await act(runSave);
    expect(updateItemExternalStock).toHaveBeenCalled();
    expect(hook.result.current.notice).toBe('外部库存配置已保存');
    expect(hook.result.current.headers[0].value).toBe('');
  });
});
