import { useEffect, useState } from 'react';
import type { ExternalStockCheckMode, ExternalStockConfig, ExternalStockHeaderDraft, Item } from './api';
import { checkItemExternalStock, getItemExternalStock, itemErrorMessage, updateItemExternalStock } from './api';

/** ExternalStockEditor 是外部库存弹窗拥有的表单、状态和动作。 */
export interface ExternalStockEditor {
  /** config 是当前草稿；加载完成前为空。 */
  config: ExternalStockConfig | null;
  /** headers 是请求头草稿，已保存的值不会回填到输入框。 */
  headers: ExternalStockHeaderDraft[];
  /** loading 表示正在读取配置。 */
  loading: boolean;
  /** saving 表示正在保存或检查。 */
  saving: boolean;
  /** error 是最近一次失败说明。 */
  error: string;
  /** notice 是最近一次成功说明。 */
  notice: string;
  /** setConfig 替换整份草稿。 */
  setConfig: (config: ExternalStockConfig) => void;
  /** setHeaders 替换请求头草稿。 */
  setHeaders: (headers: ExternalStockHeaderDraft[]) => void;
  /** save 保存当前草稿。 */
  save: () => Promise<void>;
  /** executeArmed 表示用户已点开真实执行确认，还没有提交。 */
  executeArmed: boolean;
  /** checkNow 按演练或已保存开关立即检查。 */
  checkNow: (mode: Exclude<ExternalStockCheckMode, 'execute_once'>) => Promise<void>;
  /** armExecuteOnce 打开真实执行确认，不发送请求。 */
  armExecuteOnce: () => void;
  /** cancelExecuteOnce 关闭真实执行确认。 */
  cancelExecuteOnce: () => void;
  /** confirmExecuteOnce 在确认后真实执行一次，不修改已保存演练。 */
  confirmExecuteOnce: () => Promise<void>;
}

/** useExternalStockEditor 读取并保存单个商品的外部库存配置。关闭或切换商品时丢弃过期响应。 */
export function useExternalStockEditor(item: Item | null, open: boolean): ExternalStockEditor {
  // [config, setConfig] 是当前商品的外部库存草稿。
  const [config, setConfig] = useState<ExternalStockConfig | null>(null);
  // [headers, setHeaders] 是请求头草稿，不回填已保存的秘密值。
  const [headers, setHeaders] = useState<ExternalStockHeaderDraft[]>([]);
  // [loading, setLoading] 表示配置是否仍在读取。
  const [loading, setLoading] = useState(false);
  // [saving, setSaving] 表示保存或检查是否进行中。
  const [saving, setSaving] = useState(false);
  // [error, setError] 是最近一次失败说明。
  const [error, setError] = useState('');
  // [notice, setNotice] 是最近一次成功说明。
  const [notice, setNotice] = useState('');
  // [executeArmed, setExecuteArmed] 表示真实执行确认是否已经打开。
  const [executeArmed, setExecuteArmed] = useState(false);

  useEffect(/* syncExternalStock 打开弹窗时读取配置，关闭或切换商品后丢弃过期响应。 */ () => {
    if (!open || !item) {
      setConfig(null);
      setHeaders([]);
      setError('');
      setNotice('');
      setExecuteArmed(false);
      return undefined;
    }
    // cancelled 让关闭弹窗或切换商品后的响应不能写回当前草稿。
    let cancelled = false;
    setLoading(true);
    setError('');
    setNotice('');
    getItemExternalStock(item.cookie_id, item.item_id)
      .then(/* loaded 是服务端返回的非秘密配置。 */ loaded => {
        if (cancelled) return;
        setConfig(loaded);
        setHeaders(loaded.headers.map(/* header 是已保存名称，值留空并标记保留。 */ header => ({ name: header.name, value: '', keep: true })));
      })
      .catch(/* loadError 是读取失败原因。 */ loadError => {
        if (!cancelled) setError(itemErrorMessage(loadError, '读取外部库存配置失败'));
      })
      .finally(/* finishLoad 在读取结束后关闭加载状态。 */ () => {
        if (!cancelled) setLoading(false);
      });
    return /* cancelLoad 标记这次读取已经过期。 */ () => {
      cancelled = true;
    };
  }, [open, item]);

  // save 把草稿写回服务端，并用返回的名称重建空值请求头草稿。
  const save = async () => {
    if (!item || !config || saving) return;
    setSaving(true);
    setError('');
    setNotice('');
    try {
      // saved 是保存后的非秘密配置。
      const saved = await updateItemExternalStock(item.cookie_id, item.item_id, config, headers);
      setConfig(saved);
      setHeaders(saved.headers.map(/* header 是保存后的请求头名称。 */ header => ({ name: header.name, value: '', keep: true })));
      setNotice('外部库存配置已保存');
    } catch (saveError /* 保存外部库存配置失败的原因 */) {
      setError(itemErrorMessage(saveError, '保存外部库存配置失败'));
    } finally {
      setSaving(false);
    }
  };

  // runCheck 按指定模式检查，并把非秘密结果写回草稿。
  const runCheck = async (mode: ExternalStockCheckMode) => {
    if (!item || saving) return;
    setSaving(true);
    setError('');
    setNotice('');
    try {
      // result 是本次检查结果。
      const result = await checkItemExternalStock(item.cookie_id, item.item_id, mode);
      setConfig(result.config);
      setHeaders(result.config.headers.map(/* header 是检查后的请求头名称。 */ header => ({ name: header.name, value: '', keep: true })));
      setNotice(result.message || (result.dryRun ? '演练完成' : '检查完成'));
      if (result.status === 'backed_off' || result.status === 'error') setError(result.message);
    } catch (checkError /* 立即检查失败的原因 */) {
      setError(itemErrorMessage(checkError, '外部库存检查失败'));
    } finally {
      setSaving(false);
      setExecuteArmed(false);
    }
  };

  // checkNow 立即演练，或按已保存开关检查。它不会发送 execute_once。
  const checkNow = async (mode: Exclude<ExternalStockCheckMode, 'execute_once'>) => {
    await runCheck(mode);
  };

  // armExecuteOnce 只打开确认，避免误触真实下架或上架。
  const armExecuteOnce = () => {
    if (!saving) setExecuteArmed(true);
  };

  // cancelExecuteOnce 关闭确认且不发送请求。
  const cancelExecuteOnce = () => {
    setExecuteArmed(false);
  };

  // confirmExecuteOnce 在用户确认后真实执行一次。
  const confirmExecuteOnce = async () => {
    if (!executeArmed) return;
    await runCheck('execute_once');
  };

  return { config, headers, loading, saving, error, notice, executeArmed, setConfig, setHeaders, save, checkNow, armExecuteOnce, cancelExecuteOnce, confirmExecuteOnce };
}
