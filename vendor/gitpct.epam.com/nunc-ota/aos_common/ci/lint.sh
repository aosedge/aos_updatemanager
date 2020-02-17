PKG_LIST=$(go list ./... | grep -v /vendor/)
for package in ${PKG_LIST}; do
    ONE_PKG=${package#"aos_common/"}    
    golint -set_exit_status ${ONE_PKG} ;
done
