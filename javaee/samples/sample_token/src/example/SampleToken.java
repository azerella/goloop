/*
 * Copyright 2019 ICON Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package example;

import avm.Address;
import avm.Blockchain;
import avm.DictDB;
import foundation.icon.ee.tooling.abi.EventLog;
import foundation.icon.ee.tooling.abi.External;
import foundation.icon.ee.tooling.abi.Optional;

import java.math.BigInteger;

public class SampleToken
{
    private final String name;
    private final String symbol;
    private final int decimals;
    private final BigInteger totalSupply;
    private DictDB<Address, BigInteger> balances;

    public SampleToken(String _name, String _symbol, BigInteger _decimals, BigInteger _initialSupply) {
        this.name = _name;
        this.symbol = _symbol;
        this.decimals = _decimals.intValue();

        // decimals must be larger than 0 and less than 21
        Blockchain.require(this.decimals >= 0);
        Blockchain.require(this.decimals <= 21);

        // initialSupply must be larger than 0
        Blockchain.require(_initialSupply.compareTo(BigInteger.ZERO) >= 0);

        // calculate totalSupply
        if (_initialSupply.compareTo(BigInteger.ZERO) > 0) {
            BigInteger oneToken = pow(BigInteger.TEN, this.decimals);
            this.totalSupply = oneToken.multiply(_initialSupply);
        } else {
            this.totalSupply = BigInteger.ZERO;
        }

        // set the initial balance of the owner
        this.balances = Blockchain.newDictDB("balances", BigInteger.class);
        this.balances.set(Blockchain.getOrigin(), this.totalSupply);
    }

    // BigInteger#pow() is not implemented in the shadow BigInteger.
    // we need to use our implementation for that.
    private static BigInteger pow(BigInteger base, int exponent) {
        BigInteger result = BigInteger.ONE;
        for (int i = 0; i < exponent; i++) {
            result = result.multiply(base);
        }
        return result;
    }

    @External(readonly=true)
    public String name() {
        return name;
    }

    @External(readonly=true)
    public String symbol() {
        return symbol;
    }

    @External(readonly=true)
    public int decimals() {
        return decimals;
    }

    @External(readonly=true)
    public BigInteger totalSupply() {
        return totalSupply;
    }

    @External(readonly=true)
    public BigInteger balanceOf(Address _owner) {
        return safeGetBalance(_owner);
    }

    @External
    public void transfer(Address _to, BigInteger _value, @Optional byte[] _data) {
        Address _from = Blockchain.getCaller();
        BigInteger fromBalance = safeGetBalance(_from);
        BigInteger toBalance = safeGetBalance(_to);

        // check some basic requirements
        Blockchain.require(_value.compareTo(BigInteger.ZERO) >= 0);
        Blockchain.require(fromBalance.compareTo(_value) >= 0);

        // adjust the balances
        safeSetBalance(_from, fromBalance.subtract(_value));
        safeSetBalance(_to, toBalance.add(_value));

        // if the recipient is SCORE, call 'tokenFallback' to handle further operation
        byte[] dataBytes = (_data == null) ? new byte[0] : _data;
        if (Address.isContract(_to)) {
            Blockchain.call(_to, "tokenFallback", _from, _value, dataBytes);
        }

        // emit Transfer event
        Transfer(_from, _to, _value, dataBytes);
    }

    private BigInteger safeGetBalance(Address owner) {
        return balances.getOrDefault(owner, BigInteger.ZERO);
    }

    private void safeSetBalance(Address owner, BigInteger amount) {
        balances.set(owner, amount);
    }

    @EventLog(indexed=3)
    private void Transfer(Address _from, Address _to, BigInteger _value, byte[] _data) {}
}
