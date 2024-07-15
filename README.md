<p align="center">
  <img src="https://raw.githubusercontent.com/PKief/vscode-material-icon-theme/ec559a9f6bfd399b82bb44393651661b08aaf7ba/icons/folder-markdown-open.svg" width="100" alt="project-logo">
</p>
<p align="center">
    <h1 align="center">FUSIONN</h1>
</p>
<p align="center">
    <em>Versatile Multi-Language Video Processing System</em>
</p>
<p align="center">
 <!-- local repository, no metadata badges. -->
<p>
<p align="center">
  <em>Developed with the software and tools below.</em>
</p>
<p align="center">
 <img src="https://img.shields.io/badge/Docker-2496ED.svg?style=default&logo=Docker&logoColor=white" alt="Docker">
 <img src="https://img.shields.io/badge/GitHub%20Actions-2088FF.svg?style=default&logo=GitHub-Actions&logoColor=white" alt="GitHub%20Actions">
 <img src="https://img.shields.io/badge/Go-00ADD8.svg?style=default&logo=Go&logoColor=white" alt="Go">
 <img src="https://img.shields.io/badge/Wire-000000.svg?style=default&logo=Wire&logoColor=white" alt="Wire">
</p>

<br><!-- TABLE OF CONTENTS -->
<details>
  <summary>Table of Contents</summary><br>

- [Overview](#overview)
- [Features](#features)
- [Repository Structure](#repository-structure)
- [Modules](#modules)
- [Getting Started](#getting-started)
  - [Installation](#installation)
  - [Usage](#usage)
  - [Tests](#tests)
- [Project Roadmap](#project-roadmap)
- [Contributing](#contributing)
- [License](#license)
- [Acknowledgments](#acknowledgments)

</details>
<hr>

## Overview

FusionN is a media management system that leverages various subsystems for seamless media processing. It handles data extraction (ExtractRequest), video/audio property analysis (FFprobeData), subtitle translation or merging (SubtitleProcessor), and efficient communication between these services (ProcessorSet). The application uses dependency injection (Wire) for flexibility and modularity. Notable features include a DeepL Translation API integration, automation of Docker container release, and efficient messaging service using Apprise and FastHTTP. FusionN also supports internationalization and cross-linguistic data exchange, ensuring smooth media processing across multiple environments.

---

## Features

---

## Repository Structure

```sh
└── fusionn/
    ├── .github
    │   └── workflows
    ├── LICENSE
    ├── Makefile
    ├── README.md
    ├── cmd
    │   └── fusionn
    ├── data
    │   ├── .DS_Store
    │   └── media
    ├── deploy
    │   └── Dockerfile
    ├── go.mod
    ├── go.sum
    ├── internal
    │   ├── app.go
    │   ├── consts
    │   ├── entity
    │   ├── handlers
    │   ├── processor
    │   ├── repository
    │   ├── server.go
    │   └── wire_gen.go
    └── pkg
        ├── apprise.go
        ├── deepl.go
        └── pkg_set.go
```

---

## Modules

<details closed><summary>.</summary>

| File                 | Summary                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| ---                  | ---                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| [go.mod](go.mod)     | In the `fusionn` repository, this go.mod file specifies the projects dependencies for seamless execution. It includes various packages such as GoFiber, Wire, Protobuf, AstiSub, and Sonic, which are essential for application development, subtitle handling, server interaction, and other crucial functions, thereby orchestrating the repositorys core functionality.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| [Makefile](Makefile) | The Makefile optimizes this software projects architecture by setting up Go Get for Google Wire, a popular dependency injection library. By triggering the wire' command, it automates code generation, ensuring efficient and consistent setup across the entire project.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| [go.sum](go.sum)     | In the `fusionn` repository, this specific code file is located within the `cmd/fusionn` directory and is integral to the execution of commands from the command-line interface (CLI). The main purpose of this codefile is to serve as the entry point for interacting with FusionN, a data analysis tool.This code file acts as the bridge between user input (CLI arguments) and the various functionalities within the FusionN ecosystem, orchestrating the workflow across different components of the software stack. Critical features include support for data processing tasks like pre-processing, normalization, aggregation, and visualization, ultimately helping scientists and engineers make informed decisions based on their datasets. The file also integrates with other tools in the parent repository's architecture to extend its capabilities.Key components within the FusionN ecosystem are designed for ease of use, scalability, modularity, and compatibility with popular open-source data analysis libraries, making it an efficient and flexible solution for complex datasets. The repository structure provides clear separation between various elements such as documentation (`README.md`), workflow configurations (`.github/workflows`), license information (`LICENSE`), and a Makefile to facilitate building the software, ensuring maintainability and extensibility over time. |

</details>

<details closed><summary>cmd.fusionn</summary>

| File                           | Summary                                                                                                                                                                                                                                                      |
| ---                            | ---                                                                                                                                                                                                                                                          |
| [main.go](cmd/fusionn/main.go) | This Go module bootstraps the Fusionn server application, providing a lightweight REST API gateway that serves as an entry point to the broader Fusionn ecosystem (fusionn/internal). The package imports and runs a server instance listening on port 4664. |

</details>

<details closed><summary>deploy</summary>

| File                            | Summary                                                                                                                                                                                                                                                                                                                                                                                                  |
| ---                             | ---                                                                                                                                                                                                                                                                                                                                                                                                      |
| [Dockerfile](deploy/Dockerfile) | This Dockerfile builds a lean Alpine Linux container optimized for go programming language, specifically for a project named `fusionn`. The container downloads required packages, sets the appropriate timezone (Asia/Shanghai), and constructs the Fusionn application. Once built, the containers default command runs Fusionn, enabling seamless deployment and scaling across various environments. |

</details>

<details closed><summary>internal</summary>

| File                                | Summary                                                                                                                                                                                                                                                                                                                                                                                               |
| ---                                 | ---                                                                                                                                                                                                                                                                                                                                                                                                   |
| [server.go](internal/server.go)     | In this `internal/server.go`, the software engineer utilizes dependency injection principles with Wire library to manage application setup (AppSet), ultimately configuring the Fiber web framework application, essential for executing API requests in our fusionn project architecture.                                                                                                            |
| [wire_gen.go](internal/wire_gen.go) | In this `internal/wire_gen.go` file, we generate interdependent objects within our Fusionn application, ensuring they are correctly initialized and assembled. By combining key modules such as processors, repositories, and handlers, the file enables a seamless and structured construction of our core functionality for video processing and AI tasks.                                          |
| [app.go](internal/app.go)           | Initialize and configure a Fiber web application (fusionn) for managing API requests, focusing on the merging function at /api/v1/merge path using handlers defined within the repository. This centralizes and modularizes our applications processing, while ensuring seamless integration with data processors, handlers, repositories, and external dependencies like DeepL and apprise packages. |

</details>

<details closed><summary>internal.repository</summary>

| File                                                           | Summary                                                                                                                                                                                                                                                                                                                                                                                                                               |
| ---                                                            | ---                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| [ffmpeg.go](internal/repository/ffmpeg.go)                     | Extracts subtitles from videos within the fusionn application architecture. It uses the ffmpeg command-line tool to identify and retrieve subtitle streams based on specific language criteria and stores their paths in relevant data structures. The function ExtractSubtitleStream aids in extracting a particular subtitle stream. If required, it can convert the subtitles into.ass format using ConvertSubtitleToAss function. |
| [convertor.go](internal/repository/convertor.go)               | The convertor.go file within the FusionN repository handles the conversion and translation of subtitles from traditional to simplified Chinese using deep learning APIs. It breaks down text in batches, translates each batch using DeepL API, then simplifies the text using a trandositional tool (T2S). This enhancement ensures better readability for users.                                                                    |
| [repositories_set.go](internal/repository/repositories_set.go) | Unifies and manages dependencies among core modules (Algo, Parser, FFMPEG, Convertor) within the application. This facilitates seamless interchangeability and modularity in our multimedia processing architecture (internal/repository/repositories_set.go).                                                                                                                                                                        |
| [parser.go](internal/repository/parser.go)                     | The provided `parser.go` file serves as a subtitles parser within the Fusionn repositorys internal package. It utilizes the go-astisub package to process input strings into `*astisub.Subtitles`, streamlining subtitle parsing and enhancing video content management."                                                                                                                                                             |
| [algo.go](internal/repository/algo.go)                         | This internal `algo.go` file houses a subtitles matching algorithm for video caption synchronization. It defines the IAlgo interface and its implementation to cluster subtitle chunks with tolerance time specified, ensuring seamless merging of Chinese and English captions. This contributes to the repositorys core functionality as a versatile video processing system for handling multi-language content.                   |

</details>

<details closed><summary>internal.repository.common</summary>

| File                                              | Summary                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| ---                                               | ---                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| [common.go](internal/repository/common/common.go) | Initializes an ASSubtitle struct for parsing SSA/ASS subtitles. The function takes care of default colors if an error occurs during color parsing and sets standard attributes like font size, style, and alignment. It also maps the Default style to all items within the structure and returns the initialized ASSubtitle object. The color-parsing function first normalizes the input and then converts it from hexadecimal to decimal for further decomposition. |

</details>

<details closed><summary>internal.entity</summary>

| File                                     | Summary                                                                                                                                                                                                                                                                                                                                             |
| ---                                      | ---                                                                                                                                                                                                                                                                                                                                                 |
| [request.go](internal/entity/request.go) | Empowers FusionNs media management system by defining an ExtractRequest structure for data extraction. This request is used when fetching sonarr episode file paths. Integral to the softwares core functionality, allowing seamless and efficient media processing within the application.                                                         |
| [ffmpeg.go](internal/entity/ffmpeg.go)   | This FFprobeData entity type processes video/audio properties in a video file (StreamInfo & Stream structs), providing data on CodecType, Resolution, Aspect Ratio, ColorSpace, PixelFormat, among other characteristics. It prepares the data structure for downstream operations within the media processing pipeline of the FusionN application. |

</details>

<details closed><summary>internal.processor</summary>

| File                                                    | Summary                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ---                                                     | ---                                                                                                                                                                                                                                                                                                                                                                                                                         |
| [subtitle.go](internal/processor/subtitle.go)           | Translates or merges subtitles in an episode file within the Fusionn media processing pipeline. It parses, translates (if needed), and clusters English and Simplified Chinese subtitle tracks, writing the merged results as a separate ASS file while sending a notification upon completion. This is achieved by leveraging various external packages for subtitle manipulation, translation, and notification services. |
| [processor_set.go](internal/processor/processor_set.go) | This processor sets up communication between various subsystems by defining interdependent services (ISubtitle, Subtitle). It is a vital part of the architecture that ensures efficient data processing within the FusionN application. By employing dependency injection, it enhances the codes flexibility and modularity.                                                                                               |

</details>

<details closed><summary>internal.handlers</summary>

| File                                                 | Summary                                                                                                                                                                                                                                                                                                  |
| ---                                                  | ---                                                                                                                                                                                                                                                                                                      |
| [merge.go](internal/handlers/merge.go)               | The `internal/handlers/merge.go` file defines a handler struct and its methods, acting as an interface to process subtitle merges within the FusionN application, integrating seamlessly with other system modules for efficient data integration.                                                       |
| [handlers_set.go](internal/handlers/handlers_set.go) | In this codebase, the `handlers_set.go` file within the internal/handlers package serves as a registry. It initializes and manages a collection of handlers, referred to as `Set`, using wire dependency injection for efficient handling of various functionalities throughout the Fusionn application. |

</details>

<details closed><summary>internal.consts</summary>

| File                                     | Summary                                                                                                                                                                 |
| ---                                      | ---                                                                                                                                                                     |
| [command.go](internal/consts/command.go) | Empowers Fusionn application by defining a vital DeepL Translation API endpoint for seamless cross-linguistic data exchange within the platforms internal architecture. |
| [consts.go](internal/consts/consts.go)   | Language configuration, Communication setup, Internationalization support.                                                                                              |

</details>

<details closed><summary>.github.workflows</summary>

| File                                                       | Summary                                                                                                                                                                                                                                                                                  |
| ---                                                        | ---                                                                                                                                                                                                                                                                                      |
| [docker-publish.yml](.github/workflows/docker-publish.yml) | The `docker-publish.yml` workflow script automates the Docker container image build and release process within the Fusionn repository, streamlining application deployment to multiple environments. This simplifies scaling, ensuring smooth integration across platforms and services. |

</details>

<details closed><summary>pkg</summary>

| File                         | Summary                                                                                                                                                                                                                                                                                                                                                                         |
| ---                          | ---                                                                                                                                                                                                                                                                                                                                                                             |
| [apprise.go](pkg/apprise.go) | In the FusionN repository, the pkg/apprise.go file contains an interface and implementation for a messaging service, called `Apprise`, using the `fasthttp` library for fast HTTP requests. The primary purpose of this component is to facilitate POSTing messages with JSON content to designated URLs, enabling efficient communication between system modules.              |
| [pkg_set.go](pkg/pkg_set.go) | Orchestrates dependencies between modules using Googles Wire dependency injection system for efficient management, streamlining instantiation of DeepL translation API and Apprise notification services within the fusionn' application architecture.                                                                                                                          |
| [deepl.go](pkg/deepl.go)     | This module, located at `pkg/deepl.go`, serves as a translator service for the FusionN application by interfacing with DeepLs Translate API. The Go package defines necessary types, implements an IDeepL interface, and encapsulates the API request and response structures to translate text from one language to another efficiently within FusionNs internal architecture. |

</details>

---

## Getting Started

**System Requirements:**

- **Go**: `version x.y.z`

### Installation

<h4>From <code>source</code></h4>

> 1. Clone the fusionn repository:
>
> ```console
> $ git clone ../fusionn
> ```
>
> 2. Change to the project directory:
>
> ```console
> $ cd fusionn
> ```
>
> 3. Install the dependencies:
>
> ```console
> $ go build -o myapp
> ```

### Usage

<h4>From <code>source</code></h4>

> Run fusionn using the command below:
>
> ```console
> $ ./myapp
> ```

### Tests

> Run the test suite using the command below:
>
> ```console
> $ go test
> ```

---

## Project Roadmap

- [X] `► INSERT-TASK-1`
- [ ] `► INSERT-TASK-2`
- [ ] `► ...`

---

## Contributing

Contributions are welcome! Here are several ways you can contribute:

- **[Report Issues](https://local/fusionn/issues)**: Submit bugs found or log feature requests for the `fusionn` project.
- **[Submit Pull Requests](https://local/fusionn/blob/main/CONTRIBUTING.md)**: Review open PRs, and submit your own PRs.
- **[Join the Discussions](https://local/fusionn/discussions)**: Share your insights, provide feedback, or ask questions.

<details closed>
<summary>Contributing Guidelines</summary>

1. **Fork the Repository**: Start by forking the project repository to your local account.
2. **Clone Locally**: Clone the forked repository to your local machine using a git client.

   ```sh
   git clone ../fusionn
   ```

3. **Create a New Branch**: Always work on a new branch, giving it a descriptive name.

   ```sh
   git checkout -b new-feature-x
   ```

4. **Make Your Changes**: Develop and test your changes locally.
5. **Commit Your Changes**: Commit with a clear message describing your updates.

   ```sh
   git commit -m 'Implemented new feature x.'
   ```

6. **Push to local**: Push the changes to your forked repository.

   ```sh
   git push origin new-feature-x
   ```

7. **Submit a Pull Request**: Create a PR against the original project repository. Clearly describe the changes and their motivations.
8. **Review**: Once your PR is reviewed and approved, it will be merged into the main branch. Congratulations on your contribution!

</details>

<details closed>
<summary>Contributor Graph</summary>
<br>
<p align="center">
   <a href="https://local{/fusionn/}graphs/contributors">
      <img src="https://contrib.rocks/image?repo=fusionn">
   </a>
</p>
</details>

---

## License

This project is protected under the [SELECT-A-LICENSE](https://choosealicense.com/licenses) License. For more details, refer to the [LICENSE](https://choosealicense.com/licenses/) file.

---

## Acknowledgments

- List any resources, contributors, inspiration, etc. here.

[**Return**](#-overview)

---
