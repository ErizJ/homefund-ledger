#!/usr/bin/env python3
"""端到端全流程验收测试（财会〔2020〕7号 口径）

在独立临时数据库上起后端，按专业会计流程走完全部功能：
建账 → 期初建账 → 收款/批量导入 → 维修支出分摊 → 利息/其他收入/备用金/国债
→ 利息分配 → 灭失返还/退返 → 银行对账 → 科目自定义（改名级联/删除拦截）
→ 手工凭证 → 作废留痕 → 月末结转 → 改账自动失效 → 反结转 → 年度结转/反结转
→ 三张报表/试算/对账/红线/诊断。

每步核对金额流转与最终状态，全部通过才退出 0。
用法：python3 scripts/e2e_test.py（需 PATH 中有 go）
"""
import http.cookiejar
import json
import os
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

BASE = 'http://127.0.0.1:8099'
SERVER = None
TMP = None
PASSED = 0
FAILED = []


def check(name, cond, detail=''):
    global PASSED
    if cond:
        PASSED += 1
        print(f'  ✓ {name}')
    else:
        FAILED.append(name)
        print(f'  ✗ {name}  {detail}')


def r2(v):
    return round(float(v or 0), 2)


class Api:
    def __init__(self):
        jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

    def req(self, method, path, body=None):
        data = json.dumps(body).encode() if body is not None else None
        r = urllib.request.Request(BASE + path, data=data, method=method,
                                   headers={'Content-Type': 'application/json'})
        try:
            with self.opener.open(r, timeout=20) as resp:
                raw = resp.read()
                return resp.status, (json.loads(raw) if raw else {})
        except urllib.error.HTTPError as e:
            raw = e.read()
            try:
                return e.code, json.loads(raw)
            except Exception:
                return e.code, {'error': raw.decode(errors='replace')}

    def get(self, p, params=None):
        if params:
            from urllib.parse import urlencode
            p += ('&' if '?' in p else '?') + urlencode(params)
        return self.req('GET', p)

    def post(self, p, b=None):
        return self.req('POST', p, b)

    def put(self, p, b=None):
        return self.req('PUT', p, b)

    def delete(self, p):
        return self.req('DELETE', p)


def start_server():
    global SERVER, TMP
    TMP = tempfile.mkdtemp(prefix='vfund-e2e-')
    db = os.path.join(TMP, 'vfund.db')
    # 安全守卫：只允许使用本脚本创建的临时库，绝不允许连到真实库
    # （曾因漏传 db 参数导致 go run 默认打开真实库，此处强制双保险）
    if not db.startswith(tempfile.gettempdir()):
        raise RuntimeError(f'拒绝在临时目录之外启动测试服务器：{db}')
    env = dict(os.environ, VFUND_PORT='8099')
    logf = open(os.path.join(TMP, 'server.log'), 'w')
    SERVER = subprocess.Popen(['go', 'run', '.', db], cwd='../server', env=env,
                              stdout=logf, stderr=subprocess.STDOUT)
    api = Api()
    for _ in range(90):
        try:
            code, _ = api.get('/api/session')
            if code == 401:
                return
        except Exception:
            pass
        time.sleep(0.5)
    raise RuntimeError('后端未在 45 秒内就绪，日志见 ' + os.path.join(TMP, 'server.log'))


def stop_server():
    if SERVER:
        os.killpg(os.getpgid(SERVER.pid), signal.SIGTERM)
        SERVER.wait(timeout=10)
    if TMP:
        shutil.rmtree(TMP, ignore_errors=True)


def hh_balances(api, community_id):
    code, rows = api.get(f'/api/reports/households?communityId={community_id}')
    assert code == 200, rows
    return {r['roomNo']: r2(r['balance']) for r in rows}


def main():
    api = Api()
    print('== 0. 登录与鉴权 ==')
    code, _ = api.get('/api/settings')
    check('未登录访问返回 401', code == 401, f'got {code}')
    code, r = api.post('/api/login', {'username': 'admin', 'password': 'admin'})
    check('admin 登录成功', code == 200 and r.get('ok'), str(r))
    code, r = api.post('/api/login', {'username': 'admin', 'password': 'wrong'})
    check('错误密码被拒绝', code != 200, f'got {code}')
    api.put('/api/settings', {'orgName': '测试代管单位'})

    print('== 1. 基础数据 ==')
    _, cA = api.post('/api/communities', {'name': '阳光花园', 'fundType': 'commercial'})
    _, cB = api.post('/api/communities', {'name': '公房小区', 'fundType': 'public'})
    _, bA = api.post('/api/buildings', {'communityId': cA['id'], 'name': '1栋'})
    _, bB = api.post('/api/buildings', {'communityId': cB['id'], 'name': '2栋'})
    hh = [('101', '张三', 100, 1000), ('102', '李四', 100, 1000), ('103', '王二', 100, 0)]
    for room, owner, area, opening in hh:
        code, _ = api.post('/api/households', {'communityId': cA['id'], 'buildingId': bA['id'],
                                               'roomNo': room, 'owner': owner, 'area': area, 'openingBalance': opening})
        assert code == 200
    code, _ = api.post('/api/households', {'communityId': cB['id'], 'buildingId': bB['id'],
                                           'roomNo': '201', 'owner': '王五', 'area': 80, 'openingBalance': 500})
    assert code == 200
    code, _ = api.put(f"/api/communities/{cA['id']}", {'firstRate': 10, 'publicOpening': 200})
    check('小区设置（首期标准+公共账期初）', code == 200)
    code, r = api.get('/api/settings')
    check('编制单位已生效', r.get('orgName') == '测试代管单位', str(r))

    print('== 2. 期初建账 ==')
    for c in (cA, cB):
        code, r = api.post('/api/gl/opening-balance', {'communityId': c['id']})
        check(f"期初建账 {c['name']}", code == 200 and r2(r['amount']) > 0, str(r))
    code, r = api.post('/api/gl/opening-balance', {'communityId': cA['id']})
    check('重复期初建账被拒绝', code != 200, f'got {code}')

    print('== 3. 收款：单笔 + 批量导入 ==')
    # 收款必须指定户室：先查询户 id
    _, hhA = api.get(f"/api/households?buildingId={bA['id']}")
    hid = {h['roomNo']: h['id'] for h in hhA}
    _, hhB = api.get(f"/api/households?buildingId={bB['id']}")
    hid['201'] = hhB[0]['id']
    code, v1 = api.post('/api/vouchers', {'type': 'income', 'date': '2026-10-01',
                                          'communityId': cA['id'], 'buildingId': bA['id'],
                                          'householdId': hid['101'], 'amount': 2000, 'summary': '张三缴存'})
    check('单笔收款生成凭证', code == 200 and v1.get('no'), str(v1))
    code, imp = api.post('/api/vouchers/import', {'rows': [
        {'date': '2026-10-02', 'community': '阳光花园', 'building': '1栋', 'room': '101', 'amount': 1000, 'summary': '银行代收'},
        {'date': '2026-10-02', 'community': '阳光花园', 'building': '1栋', 'room': '102', 'amount': 1500, 'summary': '银行代收'},
        {'date': '2026-10-02', 'community': '公房小区', 'building': '2栋', 'room': '201', 'amount': 800, 'summary': '银行代收'},
    ]})
    check('批量导入 3 笔全部成功', code == 200 and imp.get('inserted') == 3 and imp.get('skipped') == 0, str(imp))
    code, imp2 = api.post('/api/vouchers/import', {'rows': [
        {'date': '2026-10-02', 'community': '阳光花园', 'building': '1栋', 'room': '101', 'amount': 1000, 'summary': '银行代收'}]})
    check('重复导入被拦截', imp2.get('skipped') == 1, str(imp2))

    print('== 4. 维修支出：分摊 + 余额不足预览 ==')
    code, pv = api.post('/api/vouchers/expense-preview', {'communityId': cA['id'], 'buildingId': 0,
                                                          'scope': 'community', 'householdIds': [], 'amount': 100000})
    deficits = [t for t in pv.get('targets', []) if t.get('deficit', 0) > 0]
    check('大额支出预览提示余额不足', code == 200 and len(deficits) == 3, f'{len(deficits)} 户')
    code, ex = api.post('/api/vouchers', {'type': 'expense', 'date': '2026-10-03',
                                          'communityId': cA['id'], 'buildingId': 0, 'scope': 'community',
                                          'amount': 800, 'summary': '电梯维修', 'category': 'engineering',
                                          'payMethod': 'bank', 'confirmInsufficient': True})
    check('全体楼洞支出分摊 3 户', code == 200 and ex.get('allocations') == 3, str(ex))
    code, _ = api.post('/api/vouchers', {'type': 'expense', 'date': '2026-10-03',
                                         'communityId': cB['id'], 'buildingId': bB['id'], 'scope': 'building',
                                         'amount': 200, 'summary': '公房维修', 'payMethod': 'bank'})
    check('公房小区楼洞支出', code == 200)
    bal = hh_balances(api, cA['id'])
    check('分摊后 101 余额 3733.34', bal.get('101') == 3733.34, str(bal))
    check('分摊后 102 余额 2233.34', bal.get('102') == 2233.34, str(bal))
    check('分摊尾差归末户 103= -266.68', bal.get('103') == -266.68, str(bal))

    print('== 5. 利息 / 其他收入 / 备用金 / 国债 ==')
    code, _ = api.post('/api/vouchers', {'type': 'interest', 'date': '2026-10-04',
                                         'communityId': cA['id'], 'amount': 50, 'summary': '存款利息'})
    check('利息收入', code == 200)
    code, _ = api.post('/api/vouchers', {'type': 'fund_income', 'date': '2026-10-04',
                                         'communityId': cA['id'], 'amount': 30, 'incomeKind': 'business',
                                         'summary': '广告位出租'})
    check('经营收入（其他收入）', code == 200)
    code, _ = api.post('/api/vouchers', {'type': 'cash', 'date': '2026-10-04',
                                         'communityId': cA['id'], 'amount': 200, 'cashKind': 'withdraw',
                                         'summary': '提取备用金'})
    check('提取备用金', code == 200)
    code, _ = api.post('/api/vouchers', {'type': 'bond', 'date': '2026-10-04',
                                         'communityId': cA['id'], 'amount': 1000, 'bondKind': 'buy', 'summary': '购买国债'})
    check('购买国债', code == 200)
    code, rdBond = api.post('/api/vouchers', {'type': 'bond', 'date': '2026-10-05',
                                              'communityId': cA['id'], 'amount': 1000, 'bondKind': 'redeem',
                                              'bondInterest': 40, 'summary': '国债到期兑付'})
    check('国债到期兑付（含利息 40）', code == 200, str(rdBond))

    print('== 6. 利息分配 / 返还退返 ==')
    code, al = api.post('/api/vouchers', {'type': 'interest_alloc', 'date': '2026-10-05',
                                          'communityId': cA['id'], 'amount': 100, 'summary': '利息分配'})
    check('利息分配到 3 户', code == 200 and al.get('allocations') == 3, str(al))
    code, rf = api.post('/api/vouchers', {'type': 'refund', 'date': '2026-10-05',
                                          'communityId': cA['id'], 'buildingId': bA['id'],
                                          'householdId': hid['102'], 'amount': 300, 'refundKind': 'destroy',
                                          'summary': '房屋灭失返还'})
    check('灭失返还', code == 200, str(rf))
    code, rf2 = api.post('/api/vouchers', {'type': 'refund', 'date': '2026-10-05',
                                           'communityId': cA['id'], 'buildingId': bA['id'],
                                           'householdId': hid['101'], 'amount': 500, 'refundKind': 'return',
                                           'summary': '退返交存'})
    check('退返交存（冲减交存收入）', code == 200, str(rf2))
    bal = hh_balances(api, cA['id'])
    check('101 余额 3266.67', bal.get('101') == 3266.67, str(bal))
    check('102 余额 1966.67', bal.get('102') == 1966.67, str(bal))
    code, lc = api.get('/api/ledger/communities')
    pubA = next((x for x in lc if x['id'] == cA['id']), None)
    check('小区公共账余额 220（200+50+30+40-100）', pubA and r2(pubA['publicBalance']) == 220.0, str(pubA))

    print('== 7. 银行对账 ==')
    code, bk = api.post('/api/bank/import', {'rows': [
        {'date': '2026-10-01', 'amount': 2000, 'summary': '测试缴存'},
        {'date': '2026-10-02', 'amount': 999, 'summary': '无匹配流水'},
        {'date': '2026-10-02', 'amount': 1500, 'summary': '匹配导入'}],})
    check('银行流水导入 3 笔', code == 200 and bk.get('inserted') == 3, str(bk))
    code, cand = api.get('/api/bank/candidates', {'date': '2026-10-01', 'amount': 2000})
    check('2000 元找到候选凭证', code == 200 and len(cand) >= 1, str(cand))
    code, txns = api.get('/api/bank/txns')
    t2000 = next(t for t in txns if t['amount'] == 2000 and t['status'] == 'unmatched')
    t1500 = next(t for t in txns if t['amount'] == 1500 and t['status'] == 'unmatched')
    t999 = next(t for t in txns if t['amount'] == 999)
    code, m1 = api.post(f"/api/bank/txns/{t2000['id']}/match", {'voucherId': v1['id']})
    check('匹配 2000 → 收款凭证', code == 200, str(m1))
    _, vl = api.get('/api/vouchers?month=2026-10')
    v1500 = next(v for v in vl if v['amount'] == 1500 and v['status'] == 'normal')
    code, _ = api.post(f"/api/bank/txns/{t1500['id']}/match", {'voucherId': v1500['id']})
    check('匹配 1500 → 导入凭证', code == 200)
    code, _ = api.post(f"/api/bank/txns/{t999['id']}/ignore")
    check('忽略 999 流水', code == 200)
    _, txns = api.get('/api/bank/txns')
    st = {}
    for t in txns:
        st[t['status']] = st.get(t['status'], 0) + 1
    check('对账状态：2 已匹配 1 已忽略', st.get('matched') == 2 and st.get('ignored') == 1, str(st))

    print('== 8. 科目自定义：新增/改名级联/停启用/删除拦截 ==')
    code, sub = api.post('/api/gl/subjects', {'code': '500199', 'name': '测试维修费', 'type': 'expense', 'parent': '5001'})
    check('新增三级科目 500199', code == 200, str(sub))
    code, mv = api.post('/api/gl/manual-voucher', {'date': '2026-10-06', 'appendix': 0, 'entries': [
        {'summary': '测试费用', 'subjectCode': '500199', 'direction': 'debit', 'amount': 10},
        {'summary': '测试费用', 'subjectCode': '1001', 'direction': 'credit', 'amount': 10},
    ]})
    check('含自定义科目的手工凭证过账', code == 200 and mv.get('no'), str(mv))
    _, mvl = api.get('/api/gl/vouchers?month=2026-10')
    mvid = next(g['id'] for g in mvl if g['no'] == mv['no'])
    code, _ = api.put('/api/gl/subjects/500199', {'code': '500198', 'name': '测试维修费'})
    check('科目改名 500199→500198', code == 200)
    code, ent = api.get('/api/gl/entries?subject=500198')
    check('改码级联更新分录', code == 200 and len(ent) > 0, str(ent))
    code, _ = api.delete('/api/gl/subjects/5001')
    check('删除有子科目的 5001 被拒绝', code != 200, f'got {code}')
    code, _ = api.put('/api/gl/subjects/500198', {'enabled': False})
    check('停用科目', code == 200)
    code, _ = api.put('/api/gl/subjects/500198', {'enabled': True})
    check('重新启用科目', code == 200)
    # 作废该手工凭证（留痕），消除财务账与业务账的 10 元差异
    code, _ = api.post(f'/api/gl/vouchers/{mvid}/void', {'reason': 'e2e 测试作废'})
    check('手工凭证作废留痕', code == 200)
    code, _ = api.delete('/api/gl/subjects/500198')
    check('有分录记录（含作废留痕）科目不可删除', code != 200, f'got {code}')

    print('== 9. 作废业务凭证（留痕 + 余额回滚） ==')
    _, vl = api.get('/api/vouchers?month=2026-10')
    target = next(v for v in vl if v['amount'] == 1000 and v['status'] == 'normal' and v['type'] == 'income')
    code, vd = api.post(f"/api/vouchers/{target['id']}/void", {'reason': '录错金额'})
    check('作废导入收款凭证', code == 200, str(vd))
    code, d = api.get(f"/api/vouchers/{target['id']}")
    check('作废留痕（原因+状态）', d.get('status') == 'voided' and '录错金额' in (d.get('voidReason') or ''), str(d))
    bal = hh_balances(api, cA['id'])
    check('作废后 101 余额回滚至 2266.67', bal.get('101') == 2266.67, str(bal))

    print('== 10. 月末结转 / 改账自动失效 / 反结转 ==')
    code, tr = api.post('/api/gl/transfer', {'month': '2026-10'})
    check('月末结转生成结转凭证', code == 200 and tr.get('no'), str(tr))
    code, ps = api.get('/api/periods')
    check('结转后 periods 记录 2026-10', any(p['month'] == '2026-10' for p in ps), str(ps))
    code, _ = api.post('/api/vouchers', {'type': 'income', 'date': '2026-10-06',
                                         'communityId': cA['id'], 'buildingId': bA['id'],
                                         'householdId': hid['103'], 'amount': 100, 'summary': '补缴'})
    check('结转后补记收入', code == 200)
    code, ps = api.get('/api/periods')
    check('改账后结转自动失效（periods 清空）', not any(p['month'] == '2026-10' for p in ps), str(ps))
    code, tr2 = api.post('/api/gl/transfer', {'month': '2026-10'})
    check('重新结转成功', code == 200 and tr2.get('no'), str(tr2))
    code, _ = api.post('/api/periods/reopen', {'month': '2026-10'})
    check('反结转', code == 200)
    code, ps = api.get('/api/periods')
    check('反结转后 periods 无 2026-10', not any(p['month'] == '2026-10' for p in ps), str(ps))

    print('== 11. 年度结转 / 反年度结转 ==')
    code, cy = api.post('/api/periods/close-year', {'year': '2026'})
    check('年度结转（含月结）', code == 200 and cy.get('transferredMonths'), str(cy))
    code, ys = api.get('/api/periods/years')
    check('年度记录 2026', any(y['year'] == '2026' for y in ys), str(ys))
    code, _ = api.post('/api/periods/reopen-year', {'year': '2026'})
    check('反年度结转', code == 200)
    code, _ = api.post('/api/periods/close-year', {'year': '2026'})
    check('再次年度结转（最终状态：已年结）', code == 200)

    print('== 12. 最终状态核对 ==')
    code, tb = api.get('/api/gl/trial-balance')
    check('试算平衡', tb.get('balanced') is True, str(tb))
    code, rc = api.get('/api/gl/reconcile')
    diffs = [r for r in rc.get('rows', []) if r2(r.get('diff', 0)) != 0]
    check('财务账与业务台账全部勾稽（diff=0）', len(diffs) == 0, str(diffs))
    code, bs = api.get('/api/gl/balance-sheet?month=2026-10')
    ta = r2(bs.get('assetsTotalClosing', {}).get('total', 0))
    te = r2(bs.get('equityTotalClosing', {}).get('total', 0))
    check('资产负债表平衡', bs.get('balanced') is True and ta == te, f'{ta} vs {te}')
    check('资产总计 5420.00', ta == 5420.00, f'{ta}')
    bank = next((r for r in bs.get('assets', []) if '银行存款' in r.get('name', '')), None)
    check('银行存款 5220.00', bank and r2(bank['closTotal']) == 5220.00, str(bank))
    comm = next((r for r in bs.get('equity', []) if '商品住宅维修资金' in r.get('name', '')), None)
    check('商品住宅维修资金 4100.00', comm and r2(comm['closTotal']) == 4100.00, str(comm))
    pub = next((r for r in bs.get('equity', []) if '已售公有住房维修资金' in r.get('name', '')), None)
    check('公有住房维修资金 1100.00', pub and r2(pub['closTotal']) == 1100.00, str(pub))
    pend = next((r for r in bs.get('equity', []) if '待分配累计收益' in r.get('name', '')), None)
    check('待分配累计收益 220.00', pend and r2(pend['closTotal']) == 220.00, str(pend))
    code, ist = api.get('/api/gl/income-statement?month=2026-10')
    tot = {}
    for r in ist.get('income', []) + ist.get('expense', []):
        tot[r.get('name')] = r2(r.get('curTotal', 0))
    check('收支表：交存收入 3900', tot.get('交存收入') == 3900.00, str(tot))
    check('收支表：利息收入 50', tot.get('存款利息收入') == 50.00, str(tot))
    check('收支表：维修支出 1000', tot.get('维修支出') == 1000.00, str(tot))
    check('收支表：返还支出 300', tot.get('返还支出') == 300.00, str(tot))
    check('收支表：本期收支差额 2720', r2(ist.get('diff', {}).get('total', 0)) == 2720.00, str(ist.get('diff')))
    code, nas = api.get('/api/gl/net-asset-statement?month=2026-10')
    last = nas.get('rows', [])[-1] if nas.get('rows') else {}
    check('净资产变动表：本年年末 5420', r2(last.get('Cols', [0])[-1]) == 5420.00, str(last))
    diag = [d for d in bs.get('diagnostics', []) if d.get('level') == 'error']
    check('报表诊断无错误级提示', len(diag) == 0, str(diag))
    code, vs = api.get('/api/gl/voucher-summary?month=2026-10')
    check('凭证汇总表借贷平衡', vs.get('balanced') is True, str(vs))
    code, rp = api.get('/api/reports/households?communityId=' + str(cA['id']))
    below = [r['roomNo'] for r in rp if r.get('belowThreshold')]
    check('30% 红线：103 标红', below == ['103'], str(below))
    code, _ = api.post('/api/reports/monthly', {'month': '2026-10'})
    code, rl = api.get('/api/reports/monthly')
    names = [r['name'] for r in rl]
    check('月报表与财务报表快照已生成', any('2026-10' in n for n in names), str(names))
    check('年度财务报表快照已生成', any('2026' in n for n in names), str(names))

    print(f'\n结果：{PASSED} 项通过，{len(FAILED)} 项失败')
    if FAILED:
        print('失败项：')
        for f in FAILED:
            print('  -', f)
        return 1
    print('✅ 全流程验收通过：数据流转与最终状态全部正确')
    return 0


if __name__ == '__main__':
    if os.environ.get('VFUND_E2E_CLIENT') == '1':
        # 客户端模式：服务器已由外部启动（沙箱环境不允许脚本派生监听端口的子进程）。
        # 注意：此模式连接的 8099 服务必须是临时库，请勿指向真实数据库！
        print('【提醒】客户端模式：请确认 8099 服务运行在临时库上，切勿使用真实数据库')
        sys.exit(main())
    start_server()
    try:
        sys.exit(main())
    finally:
        stop_server()
