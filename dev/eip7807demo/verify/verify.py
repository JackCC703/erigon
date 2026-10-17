"""Cross-check the Go demo's payload root, generalized indices, and Merkle
branches against remerkleable.

Every value on the remerkleable side is recomputed from the original field
values; the only inputs taken from Go are the branch nodes themselves and the
gindex table, which is what is being checked.

    .venv/bin/python verify.py [path/to/report.json]

Exits non-zero on the first mismatch.
"""

import json
import sys

import rlp
from remerkleable.basic import uint64, uint256
from remerkleable.byte_arrays import ByteList, ByteVector, Bytes32, Bytes48, Bytes96
from remerkleable.complex import Container, List
from remerkleable.progressive import ProgressiveByteList, ProgressiveContainer, ProgressiveList

Bytes20 = ByteVector[20]

PINNED_PAYLOAD_ROOT = '51951516710af90e8c20092e086f37232c69b558d6ba0035333982e115e1f627'
PINNED_REQUESTS_ROOT = 'b1c4c413201f39ef0e4528edbd42e595a5258578a22e8634a9dee79684fdf245'
GLOAS_PAYLOAD_ROOT = '012a1eff0c6a7e4a22fb624721866d10ddb94283c678e9e0117d9fddffc43a73'


class GasAmounts(ProgressiveContainer(active_fields=[1] * 2)):
    regular: uint64
    blob: uint64


class BlobFeesPerGas(ProgressiveContainer(active_fields=[1] * 2)):
    regular: uint256
    blob: uint256


class ExecutionPayload(ProgressiveContainer(active_fields=[1] * 18)):
    parent_hash: Bytes32
    miner: Bytes20
    state_root: Bytes32
    transactions: ProgressiveList[ProgressiveByteList]
    receipts_root: Bytes32
    number: uint64
    gas_limits: GasAmounts
    gas_used: GasAmounts
    timestamp: uint64
    extra_data: ByteList[32]
    mix_hash: Bytes32
    base_fees_per_gas: BlobFeesPerGas
    withdrawals: ProgressiveList[ProgressiveByteList]
    excess_gas: GasAmounts
    parent_beacon_block_root: Bytes32
    requests_hash: Bytes32
    block_access_list: ProgressiveByteList
    slot_number: uint64


class DepositRequest(Container):
    pubkey: Bytes48
    withdrawal_credentials: Bytes32
    amount: uint64
    signature: Bytes96
    index: uint64


class WithdrawalRequest(Container):
    source_address: Bytes20
    validator_pubkey: Bytes48
    amount: uint64


class ConsolidationRequest(Container):
    source_address: Bytes20
    source_pubkey: Bytes48
    target_pubkey: Bytes48


class ExecutionRequests(Container):
    deposits: List[DepositRequest, 8192]
    withdrawals: List[WithdrawalRequest, 16]
    consolidations: List[ConsolidationRequest, 2]


class GloasRequestsSummary(ProgressiveContainer(active_fields=[1] * 5)):
    """SSZ summary of Erigon's five-field progressive request container.

    Only used to reproduce the superseded Gloas-shaped root from #23854.
    """
    deposits: Bytes32
    withdrawals: Bytes32
    consolidations: Bytes32
    builder_deposits: Bytes32
    builder_exits: Bytes32


def fake_exponential(factor, numerator, denominator):
    output, acc, i = 0, factor * denominator, 1
    while acc:
        output += acc
        acc = acc * numerator // (denominator * i)
        i += 1
    return output // denominator


def build_payload():
    """Recreate the Go fixture from its original field values."""
    to = bytes.fromhex('11' * 20)
    txs = [rlp.encode([1, 3, 21000, to, 2, b'\xaa', 0, 0, 0]),
           b'\x02' + rlp.encode([1, 2, 5, 6, 50000, to, 4, b'\xbb\xcc', [], 0, 0, 0])]
    receipts = [rlp.encode([1, 21000, bytes(256), []]), b'\x02' + rlp.encode([0, 42000, bytes(256), []])]
    withdrawals = [rlp.encode([1, 2, to, 3])]
    request = WithdrawalRequest(source_address=to, validator_pubkey=bytes(48), amount=8)
    requests = ExecutionRequests(deposits=[], withdrawals=[request], consolidations=[])
    empty_root = ProgressiveList[WithdrawalRequest]().hash_tree_root()
    gloas = GloasRequestsSummary(deposits=empty_root,
                                 withdrawals=ProgressiveList[WithdrawalRequest](request).hash_tree_root(),
                                 consolidations=empty_root, builder_deposits=empty_root, builder_exits=empty_root)
    payload = ExecutionPayload(
        parent_hash=bytes.fromhex('11' * 32), miner=bytes.fromhex('22' * 20),
        state_root=bytes.fromhex('33' * 32), transactions=txs,
        receipts_root=ProgressiveList[ProgressiveByteList](*receipts).hash_tree_root(), number=99,
        gas_limits=GasAmounts(regular=30000000, blob=262144),
        gas_used=GasAmounts(regular=42000, blob=131072),
        timestamp=1, extra_data=bytes.fromhex('deadbeef'), mix_hash=bytes.fromhex('55' * 32),
        base_fees_per_gas=BlobFeesPerGas(regular=1000000000, blob=fake_exponential(1, 4096, 10000)),
        withdrawals=withdrawals, excess_gas=GasAmounts(regular=7, blob=4096),
        parent_beacon_block_root=bytes.fromhex('44' * 32), requests_hash=requests.hash_tree_root(),
        block_access_list=b'\xc0', slot_number=12345)
    return payload, gloas


def hash_pair(left, right):
    import hashlib
    return hashlib.sha256(left + right).digest()


def fold(field_root, gindex, branch):
    """Rebuild a root from a leaf, its generalized index, and a branch ordered
    leaf-to-root."""
    root = field_root
    index = gindex
    for sibling in branch:
        root = hash_pair(root, sibling) if index % 2 == 0 else hash_pair(sibling, root)
        index >>= 1
    return root


def mix_in_active_fields(no_mix_in):
    """Prepend the active-fields level to a generalized index."""
    return no_mix_in + (1 << (no_mix_in.bit_length() - 1))


def fail(message):
    print(f'FAIL {message}')
    sys.exit(1)


def main():
    report_path = sys.argv[1] if len(sys.argv) > 1 else 'report.json'
    report = json.load(open(report_path))

    payload, gloas = build_payload()

    # 1. The pinned root, recomputed from field values only.
    root = payload.hash_tree_root().hex()
    print(f'payload root (remerkleable)  {root}')
    if root != PINNED_PAYLOAD_ROOT:
        fail(f'remerkleable payload root {root} != pinned {PINNED_PAYLOAD_ROOT}')
    if report['payload_root'].removeprefix('0x') != root:
        fail(f"Go payload root {report['payload_root']} != remerkleable {root}")

    if payload.requests_hash.hex() != PINNED_REQUESTS_ROOT:
        fail('remerkleable requests root differs from the pinned value')

    payload.requests_hash = gloas.hash_tree_root()
    gloas_root = payload.hash_tree_root().hex()
    if gloas_root != GLOAS_PAYLOAD_ROOT:
        fail(f'Gloas-shaped root {gloas_root} != {GLOAS_PAYLOAD_ROOT}')
    print(f'superseded Gloas root        {gloas_root}')
    payload.requests_hash = bytes.fromhex(PINNED_REQUESTS_ROOT)

    # 2. Every field's generalized index. remerkleable's chunk_to_gindex is the
    # independent source for the mixed column; the no-mix-in column is the same
    # tree with the mix-in level removed.
    fields = list(ExecutionPayload.fields().keys())
    if len(report['fields']) != len(fields):
        fail(f"report has {len(report['fields'])} fields, schema has {len(fields)}")
    for entry in report['fields']:
        name = entry['name']
        mixed = int(ExecutionPayload.chunk_to_gindex(fields.index(name)))
        if mixed != int(entry['gindex']):
            fail(f"{name}: Go gindex {entry['gindex']} != remerkleable {mixed}")
        no_mix_in = mixed - (1 << (mixed.bit_length() - 2)) if mixed >= 4 else mixed
        if no_mix_in != int(entry['gindex_no_mix_in']):
            fail(f"{name}: Go gindex_no_mix_in {entry['gindex_no_mix_in']} != derived {no_mix_in}")
        print(f'  {name:<24} no-mix-in {no_mix_in:>4}  gindex {mixed:>4}  ok')

    # 3. Go's branches, folded with remerkleable's own field roots and gindices,
    # must rebuild remerkleable's root. This is the independent proof check.
    for proof in report['proofs']:
        name = proof['field']
        field_root = getattr(payload, name).hash_tree_root()
        mixed = int(ExecutionPayload.chunk_to_gindex(fields.index(name)))
        branch = [bytes.fromhex(node.removeprefix('0x')) for node in proof['branch']]
        if len(branch) != mixed.bit_length() - 1:
            fail(f"{name}: branch length {len(branch)} != depth {mixed.bit_length() - 1}")
        got = fold(field_root, mixed, branch)
        if got.hex() != root:
            fail(f"{name}: folded root {got.hex()} != {root}")
        print(f"  proof {name:<24} {len(branch)} nodes -> root ok")

    print('OK all cross-checks passed')


if __name__ == '__main__':
    main()
