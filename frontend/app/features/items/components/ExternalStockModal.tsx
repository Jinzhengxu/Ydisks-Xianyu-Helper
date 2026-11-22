import { Package, Save, X } from 'lucide-react';
import React from 'react';
import { createPortal } from 'react-dom';
import type { ExternalStockHeaderDraft, Item } from '../api';
import { useExternalStockEditor } from '../useExternalStockEditor';

/** ExternalStockModalProps 描述外部库存配置弹窗的输入。 */
export interface ExternalStockModalProps {
  /** item 是正在配置的商品；为空时不渲染。 */
  item: Item | null;
  /** open 表示弹窗是否可见。 */
  open: boolean;
  /** onClose 关闭弹窗并丢弃未保存草稿。 */
  onClose: () => void;
}

/** ExternalStockModal 配置单个商品的外部库存来源，并提供立即检查和演练。 */
export const ExternalStockModal: React.FC<ExternalStockModalProps> = ({ item, open, onClose }) => {
  // editor 拥有配置草稿、请求头草稿和检查动作。
  const editor = useExternalStockEditor(item, open);
  if (!open || !item) return null;
  // config 是当前草稿，读取完成前为空。
  const config = editor.config;
  // update 用当前草稿打补丁，只在配置已经加载后调用。
  const update = (patch: Partial<NonNullable<typeof config>>) => {
    if (!config) return;
    editor.setConfig({ ...config, ...patch });
  };
  // statusText 是最近一次检查的可展示摘要。
  const statusText = config ? (config.lastError || config.lastAction || '尚未检查') : '';

  return createPortal(
    <div className="modal-overlay-centered" role="presentation">
      <div className="modal-container" role="dialog" aria-modal="true" aria-labelledby="external-stock-title" style={{ maxWidth: '640px' }}>
        <div className="modal-header flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-brand">
              <Package className="h-5 w-5 shrink-0" />
              <h3 id="external-stock-title" className="truncate text-xl font-extrabold text-gray-900">外部库存</h3>
            </div>
            <p className="mt-1 truncate text-xs text-gray-500">{item.item_title || item.item_id}</p>
          </div>
          <button type="button" className="rounded-lg p-2 text-gray-500 hover:bg-gray-100" aria-label="关闭外部库存" onClick={onClose}><X className="h-4 w-4" /></button>
        </div>
        <div className="modal-body space-y-3 text-sm">
          {editor.loading && <p>正在读取配置…</p>}
          {editor.error && <p className="text-red-600" role="alert">{editor.error}</p>}
          {editor.notice && <p className="text-emerald-700">{editor.notice}</p>}
          {config && <>
          <label className="flex items-center gap-2 font-bold"><input type="checkbox" checked={config.enabled} onChange={/* enabledChange 切换是否启用外部库存。 */ event => update({ enabled: event.target.checked })} />启用外部库存</label>
          <label className="block">库存地址<input aria-label="库存地址" className="mt-1 w-full rounded-lg border px-3 py-2" value={config.sourceUrl} onChange={/* urlChange 更新外部库存地址。 */ event => update({ sourceUrl: event.target.value })} /></label>
          <div className="grid grid-cols-2 gap-3">
            <label>可用库存路径<input aria-label="可用库存路径" className="mt-1 w-full rounded-lg border px-3 py-2" value={config.jsonPath} onChange={/* pathChange 更新 JSON 路径。 */ event => update({ jsonPath: event.target.value })} /></label>
            <label>超时（秒）<input aria-label="超时秒数" type="number" min={5} max={60} className="mt-1 w-full rounded-lg border px-3 py-2" value={config.timeoutSeconds} onChange={/* timeoutChange 更新超时秒数。 */ event => update({ timeoutSeconds: Number(event.target.value) })} /></label>
            <label>Cron<input aria-label="调度表达式" className="mt-1 w-full rounded-lg border px-3 py-2" value={config.cron} onChange={/* cronChange 更新调度表达式。 */ event => update({ cron: event.target.value })} /></label>
            <label>时区<input aria-label="时区" className="mt-1 w-full rounded-lg border px-3 py-2" value={config.timezone} onChange={/* zoneChange 更新时区。 */ event => update({ timezone: event.target.value })} /></label>
            <label>周期（分钟，0 关闭）<input aria-label="周期分钟" type="number" min={0} className="mt-1 w-full rounded-lg border px-3 py-2" value={config.intervalMinutes} onChange={/* intervalChange 更新周期间隔。 */ event => update({ intervalMinutes: Number(event.target.value) })} /></label>
          </div>
          <label className="block">User-Agent（留空使用桌面浏览器）<input aria-label="User-Agent" className="mt-1 w-full rounded-lg border px-3 py-2" value={config.userAgent} onChange={/* agentChange 更新自定义 User-Agent。 */ event => update({ userAgent: event.target.value })} /></label>
          <label className="flex items-center gap-2"><input type="checkbox" checked={config.dryRun} onChange={/* dryChange 切换演练模式。 */ event => update({ dryRun: event.target.checked })} />演练模式（只记录将要执行的动作）</label>
          <p className="text-xs text-gray-500">可用数大于 0 时重新上架原商品，小于等于 0 时下架。已在售时不提交编辑，不改价格、库存和描述。同一天相同可用数会跳过闲鱼请求，到达 cron 或点击立即检查会再执行一次。</p>
          <div>
            <div className="mb-1 font-bold">请求头（已保存的值不会回显）</div>
            {editor.headers.map(/* header 是一条请求头草稿；index 是它在列表中的位置。 */ (header, index) => (
              <div key={`${header.name}-${index}`} className="mb-2 grid grid-cols-2 gap-2">
                <input aria-label={`请求头名称 ${index + 1}`} className="rounded-lg border px-3 py-2" value={header.name} onChange={/* nameChange 修改请求头名称。 */ event => replaceHeader(editor.headers, index, { ...header, name: event.target.value, keep: false }, editor.setHeaders)} />
                <input aria-label={`请求头值 ${index + 1}`} type="password" className="rounded-lg border px-3 py-2" placeholder={header.keep ? '已保存，留空则保持不变' : '值'} value={header.value} onChange={/* valueChange 写入新的请求头值。 */ event => replaceHeader(editor.headers, index, { ...header, value: event.target.value, keep: event.target.value === '' && header.keep }, editor.setHeaders)} />
              </div>
            ))}
            <button type="button" className="text-brand" onClick={/* addHeader 增加一条空请求头。 */ () => editor.setHeaders([...editor.headers, { name: '', value: '', keep: false }])}>添加请求头</button>
          </div>
          <p className="text-xs text-gray-500">最近状态：{statusText}{config.lastAvailable !== null ? `，可用 ${config.lastAvailable}` : ''}</p>
          </>}
        </div>
        <div className="modal-footer flex flex-col gap-2">
          {editor.executeArmed && <p className="text-sm text-amber-700" role="alert">确认后会按当前外部库存真实下架或重新上架一次，不会关闭已保存的演练开关。定时检查和付款发货仍按演练执行。</p>}
          <div className="flex flex-wrap justify-end gap-2">
            <button type="button" className="rounded-lg bg-gray-100 px-3 py-2 font-bold" disabled={editor.saving} onClick={/* dryRunClick 只演练这一次。 */ () => void editor.checkNow('preview')}>演练</button>
            <button type="button" className="rounded-lg bg-gray-100 px-3 py-2 font-bold" disabled={editor.saving} onClick={/* checkClick 按已保存策略立即检查。 */ () => void editor.checkNow('saved')}>立即检查</button>
            {editor.executeArmed ? <>
              <button type="button" className="rounded-lg bg-gray-100 px-3 py-2 font-bold" disabled={editor.saving} onClick={/* cancelExecute 取消尚未发送的真实执行。 */ editor.cancelExecuteOnce}>取消</button>
              <button type="button" className="rounded-lg bg-amber-600 px-3 py-2 font-bold text-white" disabled={editor.saving} onClick={/* confirmExecute 确认后真实执行一次。 */ () => void editor.confirmExecuteOnce()}>确认执行一次</button>
            </> : <button type="button" className="rounded-lg bg-amber-50 px-3 py-2 font-bold text-amber-800" disabled={editor.saving} onClick={/* armExecute 打开真实执行确认。 */ editor.armExecuteOnce}>执行一次真实操作</button>}
            <button type="button" className="inline-flex items-center gap-1 rounded-lg bg-brand px-3 py-2 font-bold text-white" disabled={editor.saving} onClick={/* saveClick 保存外部库存配置。 */ () => void editor.save()}><Save className="h-4 w-4" />保存</button>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
};

// replaceHeader 替换指定位置的请求头草稿。
function replaceHeader(headers: ExternalStockHeaderDraft[], index: number, next: ExternalStockHeaderDraft, setHeaders: (headers: ExternalStockHeaderDraft[]) => void) {
  setHeaders(headers.map(/* current 是当前请求头；position 是它的位置。 */ (current, position) => (position === index ? next : current)));
}
