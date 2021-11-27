# 介绍

raca 元兽游戏辅助脚本

# 下载
请在bin目录下载对应操作系统的二进制文件

# 使用

## 开始游戏, 战斗中会自动升级，游戏完成后会自动合成元兽蛋（如果有碎片和药水），一般用这个就可以了

### linux/mac

./metamon --address={钱包地址} --token={accesstoken} start

### windows

./metamon.exe --address={钱包地址} --token={accesstoken} start

## 合成元兽蛋

### linux/mac

./metamon --address={钱包地址} --token={accesstoken} mint

### windows

./metamon.exe --address={钱包地址} --token={accesstoken} mint

## 升级可升级的元兽

### linux/mac

./metamon --address={钱包地址} --token={accesstoken} updatelevel

### windows

./metamon.exe --address={钱包地址} --token={accesstoken} updatelevel

## 开蛋（一次开完）

### linux/mac

./metamon --address={钱包地址} --token={accesstoken} openegg

### windows

./metamon.exe --address={钱包地址} --token={accesstoken} openegg
