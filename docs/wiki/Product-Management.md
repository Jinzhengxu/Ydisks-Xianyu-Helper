# Product Management Guide

[中文](商品管理.zh-CN) | English

## Sync existing products

1. Open Product List in the sidebar; the page title is Product Management.
2. Select the accounts to sync in Sync Product Accounts.
3. Click Sync Products and wait for completion.
4. Use the product-list filter by account and confirm titles, prices, images, and product IDs.

The page also offers an automation-rule filter (to find products with no trade automation at all, or missing one of paid delivery, review gift, review reminder, or order-created price change) and a display mode (card or list; the list mode shows 20 products per page with the rule count for each of the four trade automations and the AI customer-service state).

The button at the bottom of a product card reads “Link Delivery Rule” when the product has no associated rule at all, “View Delivery Rules” when it has rules but no paid-delivery rule, and “N delivery rules” when it has paid-delivery rules. The card also shows “N rules” or “Not configured”.

## Add a product manually

Add Product creates a local association for a product that already exists on Xianyu but is not recorded in the system. Select the owning account and enter the Xianyu product ID (both required), optionally fill price, title, and image URL, then save and configure automation.

## Publish one product

1. Click Publish Product and select the publishing account.
2. Enter title, inventory, description (the title is used when blank), price, original price (optional), and postage mode. The optional shipping origin may be omitted for a virtual product.
3. Optionally enter a category keyword and fetch a category. One request returns the recommended category plus the other candidates Xianyu offers; when more than one exists, candidate buttons appear under the selected category so you can switch to another. If blank, Xianyu attempts automatic recognition, then uses Electronic Materials as the fallback.
4. Add specifications when needed. Up to two specification types are supported, with at least two values per type, and at most 1500 combinations in total.
5. Fill price and inventory for each generated combination; upload specification images when needed.
6. Upload product images. Up to nine are supported; the first is the main image. Images can be added, removed, or reordered.
7. Recheck account, category, inventory, and images, then click Publish to Xianyu. After a successful publish, the product’s delivery-rule configuration opens automatically.

Single-specification products use one total inventory value. Multi-specification inventory is the sum of combination inventory. After publishing, associate paid-delivery, review-gift, or review-reminder rules in Automation.

### Postage mode

A single listing offers four modes: **free shipping**, **by distance**, **fixed postage** (enter an amount greater than 0) and **no shipping needed** (typical for virtual goods). Product templates and bulk listing support the same modes; see [Bulk Listing](Bulk-Listing) for the table values.

## Edit and delete

- Edit on a product card changes title, price, category, or description.
- AI Customer Service on a product card sets whether AI replies for that product (Inherit Account, Force Enable, or Force Disable) and stores product-specific reference material for AI.
- Stock on a product card adjusts the quantity on Xianyu (single-variant: one number from 1 to 999999; multi-variant: one number per variant from 0 to 999999, at least one above 0). Opening the dialog sends one read request to Xianyu; saving sends one read and one write request, never retried automatically. If the stock changed on Xianyu in the meantime the dialog asks you to reload instead of overwriting. Some categories cannot be edited from the web edition; the dialog then asks you to change the stock in the Xianyu app. To sell out a product, take it off the shelf instead of setting the stock to 0. If Xianyu asks for security verification or the account session has expired, complete verification in the Xianyu app or sign the account in again before retrying.
- Delete (the trash icon shown on hover on a card, or in the actions column in list mode) removes only the local record; it does not delete the product on Xianyu. A confirmation prompt appears; confirm that orders and rules will not be affected.
- Use Link Delivery Rule/View Delivery Rules/N delivery rules to inspect the product rules. For specification products, verify every specification match.

## External stock

External Stock on a product card is opt-in per product. The assistant sends `GET` to an external JSON document and reads `available`, or `data.available` when the top-level field is absent. When the available count is 0 or below and the original listing is still on sale, it unpublishes that same listing. When the count is above 0 and the listing is off sale, it puts the same original item back on sale. It does not publish a new listing, and it never relists while the available count is 0. A product that enables this skips the account-level auto relist after sale.

You can set the URL, header names (values are encrypted and are not shown again), JSON path, timeout (default 20 seconds, allowed 5 to 60), User-Agent (blank uses a desktop browser), cron (default `1 0 * * *`), timezone (default `Asia/Shanghai`), periodic minutes (default 30, 0 turns the period off), dry-run, and optional platform-stock sync capped by the configured maximum. A check runs after paid delivery succeeds, when the schedule is due, and when you click Check now. Dry-run still reads the external API and the current listing state, but it does not change the listing.

On-sale state is read from the web edit detail `itemStatus`: `1` and `0` both mean on sale, and `-2` means off sale. When that detail is unrecognized or web edit is unsupported, the check uses the synced list `item_status`, and only `0` counts as on sale. The status text names which source was tried and the raw code, without Cookie or header values. If Xianyu returns `FAIL_BIZ_PC_NOT_SUPPORT_PUBLISH_OR_EDIT`, the dialog keeps that status and stops relisting. Virtual goods and 鱼小铺 listings often cannot be edited on the web. Risk control, HTTP 5xx, or an unknown state from both sources stops that product and backs off for 30 minutes. The backoff applies only to this stock sync and does not delay paid auto-delivery for the same item. Each scheduler pass handles at most 3 opted-in products.

The operations are `GET`/`PUT` `/api/v1/items/{cookie_id}/{item_id}/external-stock` and `POST` `/api/v1/items/{cookie_id}/{item_id}/external-stock/check`. Saving or checking needs `items:external-stock`; reading the config uses `items:read`. Responses do not include header values.

## Bulk listing

See [Bulk Listing](Bulk-Listing). A bulk task runs preflight validation first; each row is marked publishable or needing correction. After start only publishable rows are published, and rows needing correction are not published, so fix the table and run preflight again. Its automatic-delivery columns reference card-group IDs from Card Inventory.
