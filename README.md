
![wscast](https://github.com/yasutakatou/wscast/blob/pictu/capture2.gif)

#### ざっくりした動かしかた

golangでbuildしたバイナリを動かしてください。<br>
ライブラリが足りないのはgoが新しければGo Modulesで自動で解決されるし、古ければgo getなりで環境を整えてください。<br>
Windows用にコンパイルするなら以下のようにクロスコンパイルします。<br>

GOOS=windows GOARCH=386 CC=i686-w64-mingw32-gcc go build wscast.go

チェックサム確認用に使っているwhich cksumを起動時に実行して失敗するようならWindows環境と判別します。<br>
ツール作った当時はcksumデフォでしたが、新しいLinuxには入っていないこともあるようでパッケージマネージャなりから入れてください。<br>

クライアント側にしたい場合は環境変数SERVERを設定します。

export SERVER=192.168.0.100:8080<br>
(Windowsならset SERVER)

指定しないと8080で待ち受けるサーバーになります。<br>
config.iniがバイナリ起動時に読めないと起動できないのでバイナリと同じフォルダに置いてください。

Jupyterのkernelは作り方が良く分からないので

https://github.com/jupyter/echo_kernel

を組み込んでこのリポジトリのkernel.pyを上書きしてあげれば**とりあえず**動きます
