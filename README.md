**jlink.online** is a HTTP microservice that builds optimized/minimized Java runtimes on the fly.

:boom: This project is currently experimental and subject to change at any time. :boom:

## Introduction
This project is a wrapper for Java's `jlink` utility that makes it faster and easier to build custom Java runtimes for an application. Just send it a HTTP request and **jlink.online** fetches the appropriate JDK [from AdoptOpenJDK](https://github.com/AdoptOpenJDK), runs `jlink` to produce a custom runtime image containing your dependencies, then returns that compressed runtime in the response.

Using an optimized runtime is a good idea when deploying to a production environment or when bundling a platform-specific runtime to distribute with your application. For many applications, a `jlink`'d runtime will be significantly smaller in size.

## Usage Examples
#### Download a minimized Java 11 runtime for Linux x64 (containing `java.base` only)
```
https://jlink.online/x64/linux/11.0.8+10
```

#### Download a minimized Java 11 runtime for Linux x64 (containing `java.desktop` and `jdk.zipfs`)
```
https://jlink.online/x64/linux/11.0.8+10?modules=java.desktop,jdk.zipfs
```

#### Download a minimized runtime in a Dockerfile
```sh
# If you do 'FROM openjdk' then you'll get a full runtime
FROM alpine:latest

# Install dependencies
RUN apk add curl

# Install custom runtime
RUN curl -G 'https://jlink.online/x64/linux/11.0.8+10' \
    -d modules=java.base \
    | tar zxf -

# Install application
# ...
```

#### Upload your application's `module-info.java` (experimental)
Suppose your application has the following module definition:
```java
module com.github.example {
    requires org.slf4j;
    ...
}
```

Then to build a custom runtime containing your dependencies, you can send a POST request containing your `module-info.java`:
```sh
curl --data-binary @com.github.example/src/main/java/module-info.java \
  'https://jlink.online/x64/linux/11.0.8+10?artifacts=org.slf4j:slf4j-api:2.0.0-alpha1' \
  --output app_runtime.tar.gz
```

The `artifacts` parameter is a comma-separated list of the Maven Central coordinates of your dependencies. This is required to know what versions to include in your runtime. In the future, we may be able to get this information from a `build.gradle` or `pom.xml` which would be much more convenient.

**Unfortunately this can't work for dependencies that are automatic modules (because automatic modules don't specify *their* dependencies).**

## Credits
Thanks to the following projects:

- [gin](https://github.com/gin-gonic/gin) - for handling HTTP requests
- [jlink](https://docs.oracle.com/javase/9/tools/jlink.htm) - for minimizing runtime images
- [testify](https://github.com/stretchr/testify) - for unit testing
