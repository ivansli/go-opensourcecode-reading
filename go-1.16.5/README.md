# Go1.16.5 源码阅读

Go源码目录结构如下：

```
.
├── AUTHORS // 官方的Go语言作者
├── CONTRIBUTING.md
├── CONTRIBUTORS // 第三方的贡献者
├── LICENSE  // 协议
├── PATENTS // 专利
├── README.md
├── SECURITY.md
├── VERSION
├── api  // Golang每个版本的功能列表归档文件
├── bin  // 存放所有官方提供的Go语言相关工具的可执行文件
├── doc  // Golang文档说明
├── favicon.ico
├── lib
├── misc // 存放各类编辑器或者IDE（集成开发环境）软件的插件，辅助查看和编辑Go代码
├── pkg // 标准库的所有归档文件,目录结构跟当前使用的操作系统相关
├── robots.txt
├── src  // 核心：存放所有标准库、Go语言工具、相关底层库源码
│   ├── Make.dist
│   ├── README.vendor
│   ├── all.bash
│   ├── all.bat
│   ├── all.rc
│   ├── archive  //归档文件处理库，可以用来处理tar与zip类型文件
│   ├── bootstrap.bash
│   ├── bufio //用于文本的读取写入
│   ├── buildall.bash
│   ├── builtin //常用了内置类型、函数和接口，比如make、new、len、error等
│   ├── bytes //操作字节的函数
│   ├── clean.bash
│   ├── clean.bat
│   ├── clean.rc
│   ├── cmd //Go语言的基本工具
│   ├── cmp.bash
│   ├── compress //压缩、解压工具，支持bzip2、flate、gzip、lzw、zlib等格式
│   ├── container //双向链表（list）、堆（heap）、环形联表（ring）的数据结构的操作
│   ├── context //上下文操作
│   ├── crypto  //很多加解密算法，比如rsa、sha1、aes、md5等函数
│   ├── database //提供了各种数据库的通用API
│   ├── debug  //支持Go程序调试
│   ├── embed
│   ├── encoding //各类编码的实现，比如base64、json、xml、hex
│   ├── errors //经常使用的错误函数
│   ├── expvar //一系列标准接口，可以通过HTTP的方式将服务器的变量以JSON格式打印出来
│   ├── flag //解析处理命令行参数的工具
│   ├── fmt  //各种格式化输出方法
│   ├── go
│   ├── go.mod
│   ├── go.sum
│   ├── hash  //封装了crc32、crc64在内的哈希函数
│   ├── html //HTML模板引擎，可以将代码与HTML混合在一起，它会负责解析转义
│   ├── image //图像处理库
│   ├── index //用来实现字符串高速匹配查找
│   ├── internal  //专门用来控制包导入权限的
│   ├── io  //文件I/O提供了一些基本的接口
│   ├── log //日志记录方法
│   ├── make.bash
│   ├── make.bat
│   ├── make.rc
│   ├── math //基本的数学相关的函数
│   ├── mime //MIME类型的解析
│   ├── net //各种网络IO的函数
│   ├── os //用来操作操作系统的命令
│   ├── path //用于处理斜杠分隔符路径的函数
│   ├── plugin //Go1.8版本以后提供的插件机制，可以动态地加载动态链接库文件.so
│   ├── race.bash
│   ├── race.bat
│   ├── reflect //反射读取方法
│   ├── regexp //正则表达式的实现
│   ├── run.bash
│   ├── run.bat
│   ├── run.rc
│   ├── runtime //`核心`！！！Go运行时操作
│   ├── sort  //部分排序算法
│   ├── strconv  //类型与字符串互相转换的方法
│   ├── strings //字符串操作的相关方法
│   ├── sync   //基本的同步机制，各种锁的实现
│   ├── syscall //一系列系统调用的接口
│   ├── testdata
│   ├── testing
│   ├── text
│   ├── time  //时间处理相关的函数
│   ├── unicode
│   ├── unsafe // 一些不安全的操作场景,例如指针操作
│   └── vendor
└── test // Golang单元测试程序
```

src为go源码所在目录，也是源码追溯的核心位置。
<br/>
<br/>

---

# 源码追溯与总结

1. [初识Golang汇编](https://mp.weixin.qq.com/s?__biz=Mzk0MzE2Mjc0NQ==&mid=2247483941&idx=1&sn=b57cadba61d6ab369e3fd50fd1aa8657&chksm=c3395633f44edf25a4019fe2f855cdc761cd05d26d359888dd217a39bf5def5bcfdcce25f03b&token=1036848849&lang=zh_CN#rd)
2. [Go源码的一次追溯](./thedoc/go源码的一次追溯.md)
3. [追溯Go中sysmon的启动过程](./thedoc/追溯Go中sysmon的启动过程.md)
4. [通过抓包认识gRpc](./thedoc/通过抓包认识gRpc.md)
